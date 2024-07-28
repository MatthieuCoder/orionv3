package implementation

import (
	"crypto/x509"
	"fmt"
	"os"

	"github.com/MatthieuCoder/OrionV3/bin/registry/implementation/session"
	"github.com/MatthieuCoder/OrionV3/internal"
	"github.com/MatthieuCoder/OrionV3/internal/proto"
	"github.com/rs/zerolog/log"
)

type OrionRegistryImplementation struct {
	rootCertPool   *x509.CertPool
	sessionManager *session.SessionManager
	proto.UnimplementedRegistryServer
}

func NewOrionRegistryImplementation() (*OrionRegistryImplementation, error) {
	// Reading the root certificate
	ca, err := os.ReadFile(*internal.AuthorityPath)
	if err != nil {
		log.Debug().
			Err(err).
			Msg("failed to import the root ca certificate")
		return nil, err
	}

	// Create a new certificate pool containing the root certificates
	root := x509.NewCertPool()
	// Append the root certificate to the pool
	ok := root.AppendCertsFromPEM(ca)
	if !ok {
		return nil, fmt.Errorf("the root certificate failed to be imported")
	}

	return &OrionRegistryImplementation{
		sessionManager: session.NewSessionManager(),
		rootCertPool:   root,
	}, nil
}

func (r *OrionRegistryImplementation) SubscribeToStream(subscibeEvent proto.Registry_SubscribeToStreamServer) error {
	// Used to store the current session
	var currentSession *session.Session
	// Used to handle the events
	eventsStream := make(chan *proto.RPCClientEvent)

	// Simple subroutine to handle end various events
	go func() {
		for {
			event, err := subscibeEvent.Recv()
			if err != nil {
				return
			}

			eventsStream <- event
		}
	}()

	select {
	case clientEvent := <-eventsStream:
		if event := clientEvent.GetInitialize(); event != nil && currentSession == nil {
			session, err := session.New(
				r.sessionManager,
			)
			if err != nil {
				return err
			}
			err = session.Authenticate(
				event,
				r.rootCertPool,
			)
			if err != nil {
				return err
			}

			// Set the session
			currentSession = session
			// we start a routine to listen to the send stream
			go func() {
				for send := range currentSession.Ch() {
					err := subscibeEvent.Send(send)

					if err != nil {
						return
					}
				}
			}()

			// Start the disposal when exiting the routine
			defer currentSession.Dispose()
		}
	case <-subscibeEvent.Context().Done():
		return subscibeEvent.Context().Err()
	}

	for {
		select {
		// Handle all the events from the client
		case event := <-eventsStream:
			err := currentSession.HandleClientEvent(event)
			if err != nil {
				return err
			}

		case <-subscibeEvent.Context().Done():
			return subscibeEvent.Context().Err()
		}
	}
}
