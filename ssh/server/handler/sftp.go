package handler

import (
	gliderssh "github.com/gliderlabs/ssh"
	"github.com/shellhub-io/shellhub/pkg/httptunnel"
	"github.com/shellhub-io/shellhub/ssh/pkg/flow"
	"github.com/shellhub-io/shellhub/ssh/pkg/metadata"
	"github.com/shellhub-io/shellhub/ssh/session"
	log "github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
)

const SFTPSubsystem = "sftp"

// SFTPSubsystemHandler handlers a SFTP connection.
func SFTPSubsystemHandler(_ *httptunnel.Tunnel) gliderssh.SubsystemHandler {
	return func(client gliderssh.Session) {
		log.WithFields(log.Fields{"sshid": client.User()}).Info("SFTP connection started")
		defer log.WithFields(log.Fields{"sshid": client.User()}).Info("SFTP connection closed")

		defer client.Close()

		// TODO:
		sess := client.Context().Value("session").(*session.Session)
		sess.SetClientSession(client)

		agent, reqs, err := sess.NewAgentSession()
		if err != nil {
			echo(sess.UID, client, err, "Error when trying to start the agent's session")

			return
		}
		defer agent.Close()

		if err := connectSFTP(sess, reqs); err != nil {
			echo(sess.UID, client, err, "Error during SSH connection")

			return
		}
	}
}

func connectSFTP(sess *session.Session, reqs <-chan *gossh.Request) error {
	api := metadata.RestoreAPI(sess.Client.Context())

	log.WithFields(log.Fields{"session": sess.UID, "sshid": sess.Client.User()}).
		Debug("requesting a subsystem for session")

	if err := sess.Agent.RequestSubsystem(SFTPSubsystem); err != nil {
		log.WithError(err).
			WithFields(log.Fields{"session": sess.UID, "sshid": sess.Client.User()}).
			Error("failed to request a subsystem")

		return err
	}

	go session.HandleRequests(sess.Client.Context(), reqs, api, sess.Client.Context().Done())

	if errs := api.SessionAsAuthenticated(sess.UID); len(errs) > 0 {
		log.WithError(errs[0]).
			WithFields(log.Fields{"session": sess.UID, "sshid": sess.Client.User()}).
			Error("failed to authenticate the session")

		return errs[0]
	}

	flw, err := flow.NewFlow(sess.Agent)
	if err != nil {
		log.WithError(err).
			WithFields(log.Fields{"session": sess.UID, "sshid": sess.Client.User()}).
			Error("failed to create a flow of data from agent")

		return err
	}

	done := make(chan bool)

	go flw.PipeIn(sess.Client, done)
	go flw.PipeOut(sess.Client, done)
	go flw.PipeErr(sess.Client, done)

	<-done
	<-done
	<-done

	return nil
}
