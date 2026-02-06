package audio

import (
	"io"
	"mime/multipart"
)

// MultipartRewriter rewrites a parsed multipart form into a new multipart body
// with an updated model field.
type MultipartRewriter struct {
	writer      *multipart.Writer
	form        *multipart.Form
	actualModel string
	pw          io.Writer
}

// NewMultipartRewriter creates a new multipart rewriter.
func NewMultipartRewriter(pw io.Writer, form *multipart.Form, actualModel string) *MultipartRewriter {
	return &MultipartRewriter{
		writer:      multipart.NewWriter(pw),
		form:        form,
		actualModel: actualModel,
		pw:          pw,
	}
}

// ContentType returns the content type header value for the new multipart body.
func (r *MultipartRewriter) ContentType() string {
	return r.writer.FormDataContentType()
}

// Write writes the multipart form data to the pipe.
func (r *MultipartRewriter) Write() error {
	defer r.writer.Close()

	// Write all form fields, replacing the model field
	if r.form.Value != nil {
		for key, values := range r.form.Value {
			for _, value := range values {
				if key == "model" {
					// Replace with the actual model name (prefix stripped)
					if err := r.writer.WriteField(key, r.actualModel); err != nil {
						return err
					}
				} else {
					if err := r.writer.WriteField(key, value); err != nil {
						return err
					}
				}
			}
		}
	}

	// Write all file fields
	if r.form.File != nil {
		for key, fileHeaders := range r.form.File {
			for _, fileHeader := range fileHeaders {
				// Open the uploaded file
				file, err := fileHeader.Open()
				if err != nil {
					return err
				}

				// Create a form file in the new multipart
				part, err := r.writer.CreateFormFile(key, fileHeader.Filename)
				if err != nil {
					file.Close()
					return err
				}

				// Copy the file content
				if _, err := io.Copy(part, file); err != nil {
					file.Close()
					return err
				}

				file.Close()
			}
		}
	}

	return nil
}
