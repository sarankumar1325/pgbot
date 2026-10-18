// Package app exposes QueryShift's core workflows without coupling them to a
// particular interface. The Cobra CLI and the full-screen TUI can both call
// these services without parsing one another's output.
package app

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/sarankumar1325/queryshift/internal/collect"
	"github.com/sarankumar1325/queryshift/internal/conn"
	"github.com/sarankumar1325/queryshift/internal/findings"
	"github.com/sarankumar1325/queryshift/internal/model"
	"github.com/sarankumar1325/queryshift/internal/render"
)

// Inspection is one ephemeral, read-only database reading. Connection
// credentials are never included in the result and are not persisted.
type Inspection struct {
	Context    *model.Context
	Report     string
	Host       string
	PoolerNote string
}

// Inspect connects read-only, collects a short workload sample, computes the
// deterministic findings, renders the full report, and closes the pool.
func Inspect(ctx context.Context, connectionString string, width int) (*Inspection, error) {
	connectionString = stripUnsupportedConnectionParams(strings.TrimSpace(connectionString))
	if connectionString == "" {
		return nil, fmt.Errorf("connection string is required")
	}

	target, err := conn.Connect(ctx, connectionString)
	if err != nil {
		return nil, fmt.Errorf("connect: %s", conn.RedactConnString(err.Error()))
	}
	defer target.Close()

	c, err := collect.Run(ctx, target, collect.Options{
		Interval: time.Second,
		Deadline: 60 * time.Second,
		ASHHz:    0,
	})
	if err != nil {
		return nil, fmt.Errorf("collect: %s", conn.RedactConnString(err.Error()))
	}
	c.Server.ViaPooler = target.Pooler.Detected
	c.Findings = findings.Compute(c)

	host := target.Pool.Config().Host
	var report bytes.Buffer
	if err := render.Terminal(&report, c, render.Options{
		Color: false,
		Full:  true,
		Host:  host,
		Width: width,
	}); err != nil {
		return nil, fmt.Errorf("render report: %w", err)
	}

	result := &Inspection{Context: c, Report: report.String(), Host: host}
	if target.Pooler.Detected {
		result.PoolerNote = target.Pooler.Note()
	}
	return result, nil
}

// stripUnsupportedConnectionParams removes libpq-only hardening parameters the
// current driver cannot honor. TLS remains controlled by sslmode. Doing this in
// the application layer also prevents a library warning from corrupting a TUI.
func stripUnsupportedConnectionParams(connectionString string) string {
	if strings.HasPrefix(connectionString, "postgres://") || strings.HasPrefix(connectionString, "postgresql://") {
		u, err := url.Parse(connectionString)
		if err != nil {
			return connectionString
		}
		q := u.Query()
		q.Del("channel_binding")
		u.RawQuery = q.Encode()
		return u.String()
	}
	fields := strings.Fields(connectionString)
	kept := fields[:0]
	for _, field := range fields {
		if !strings.HasPrefix(field, "channel_binding=") {
			kept = append(kept, field)
		}
	}
	return strings.Join(kept, " ")
}
