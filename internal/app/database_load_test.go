package app

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestStreamLoadDatabaseTimesOutStalledHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	release := make(chan struct{})
	defer close(release)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		<-release // Accept the socket but never complete the PostgreSQL handshake.
	}()

	dsn := "postgres://queryshift@" + listener.Addr().String() + "/app?sslmode=disable"
	started := time.Now()
	updates := streamLoadDatabase(context.Background(), dsn, 50*time.Millisecond, time.Second)

	var loadErr error
	for update := range updates {
		if update.Err != nil {
			loadErr = update.Err
		}
	}
	if loadErr == nil {
		t.Fatal("expected stalled connection to time out")
	}
	if !strings.Contains(loadErr.Error(), "connection timed out after 50ms") {
		t.Fatalf("unexpected timeout error: %v", loadErr)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("connection timeout took %s, want a prompt failure", elapsed)
	}
}
