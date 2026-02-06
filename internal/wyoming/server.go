package wyoming

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Handler verarbeitet eingehende Wyoming-Verbindungen.
type Handler interface {
	// HandleConnection verarbeitet eine einzelne TCP-Verbindung.
	// Der Reader/Writer wird vom Server bereitgestellt.
	HandleConnection(ctx context.Context, reader *bufio.Reader, writer io.Writer) error

	// ServiceType gibt den Diensttyp zurück (z.B. "stt", "tts", "wake").
	ServiceType() string
}

// Server ist ein TCP-Server für das Wyoming-Protokoll.
type Server struct {
	addr     string
	handler  Handler
	listener net.Listener
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewServer erstellt einen neuen Wyoming-TCP-Server.
func NewServer(addr string, handler Handler) *Server {
	return &Server{
		addr:    addr,
		handler: handler,
	}
}

// Start startet den TCP-Server.
func (s *Server) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	var err error
	s.listener, err = net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("Wyoming %s: Fehler beim Starten des Listeners auf %s: %w", s.handler.ServiceType(), s.addr, err)
	}

	log.Infof("Wyoming %s Server gestartet auf %s", s.handler.ServiceType(), s.addr)

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Stop stoppt den TCP-Server.
func (s *Server) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			log.Warnf("Wyoming %s: Fehler beim Schließen des Listeners: %v", s.handler.ServiceType(), err)
		}
	}
	s.wg.Wait()
	log.Infof("Wyoming %s Server gestoppt", s.handler.ServiceType())
	return nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				log.Errorf("Wyoming %s: Fehler beim Akzeptieren der Verbindung: %v", s.handler.ServiceType(), err)
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConnection(conn)
		}()
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	log.Debugf("Wyoming %s: Neue Verbindung von %s", s.handler.ServiceType(), remoteAddr)

	reader := bufio.NewReaderSize(conn, 65536)

	if err := s.handler.HandleConnection(s.ctx, reader, conn); err != nil {
		if err != io.EOF {
			log.Debugf("Wyoming %s: Verbindung von %s beendet: %v", s.handler.ServiceType(), remoteAddr, err)
		}
	}

	log.Debugf("Wyoming %s: Verbindung von %s geschlossen", s.handler.ServiceType(), remoteAddr)
}
