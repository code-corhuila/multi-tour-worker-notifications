// Package out holds the ports the application needs from the outside, defined by the consumer.
package out

import "context"

// Message is one e-mail: the visible name of the sender (the address is the platform's, set by the adapter), the one recipient,
// the subject and the HTML body.
type Message struct {
	FromName string
	To       string
	Subject  string
	HTMLBody string
}

// Mailer sends one message. An error means it was not accepted by the server; nothing is retried here (job 7 retries).
type Mailer interface {
	Send(ctx context.Context, m Message) error
}
