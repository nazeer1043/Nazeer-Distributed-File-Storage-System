package p2p

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTCPTransport(t *testing.T) {
	tr := NewTCPTransport(
		WithListenAddr(":0"),
		WithHandshakeFunc(NOPHandshakeFunc),
		WithDecoder(&DefaultDecoder{}),
	)
	defer tr.Close()

	assert.NotEmpty(t, tr.ListenAddr)
	assert.Nil(t, tr.ListenAndAccept())
}
