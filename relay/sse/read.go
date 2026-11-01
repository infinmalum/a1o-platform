// Package sse decodes SSE framing without interpreting provider payloads.
package sse

import (
	"bufio"
	"io"
	"strings"
)

type Event struct{ Name, Data string }

// Read delivers each event as it arrives. Returning io.EOF from consume stops
// at a protocol terminal event without waiting for the upstream to disconnect.
func Read(r io.Reader, consume func(Event) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	var name string
	var data []string
	dispatch := func() error {
		if len(data) == 0 {
			name = ""
			return nil
		}
		err := consume(Event{Name: name, Data: strings.Join(data, "\n")})
		name = ""
		data = nil
		return err
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			name = value
		case "data":
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	err := dispatch()
	if err == io.EOF {
		return nil
	}
	return err
}
