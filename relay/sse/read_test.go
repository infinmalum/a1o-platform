package sse

import (
	"io"
	"strings"
	"testing"
)

func TestFramingPreservesPayload(t *testing.T) {
	var events []Event
	err := Read(strings.NewReader(": ping\r\nevent: output\r\ndata:  leading\r\ndata: second\r\n\r\nevent: done\ndata: {}"), func(e Event) error { events = append(events, e); return nil })
	if err != nil || len(events) != 2 || events[0].Data != " leading\nsecond" || events[1].Name != "done" {
		t.Fatalf("%+v %v", events, err)
	}
	err = Read(strings.NewReader("data: stop\n\ndata: ignore\n\n"), func(e Event) error {
		if e.Data != "stop" {
			t.Fatal(e)
		}
		return io.EOF
	})
	if err != nil {
		t.Fatal(err)
	}
}
