package provider

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

const defaultSSEMaxEventBytes = 64 << 20

type sseEvent struct {
	Event string
	Data  string
}

type sseDecoder struct {
	reader        *bufio.Reader
	maxEventBytes int
	eventName     string
	dataLines     []string
	bufferedBytes int
}

func newSSEDecoder(reader io.Reader, maxEventBytes int) *sseDecoder {
	if maxEventBytes <= 0 {
		maxEventBytes = defaultSSEMaxEventBytes
	}
	return &sseDecoder{
		reader:        bufio.NewReaderSize(reader, 64*1024),
		maxEventBytes: maxEventBytes,
	}
}

func (d *sseDecoder) Next() (sseEvent, bool, error) {
	for {
		line, ok, err := d.readLine()
		if err != nil {
			return sseEvent{}, false, err
		}
		if !ok {
			return d.dispatch()
		}
		if line == "" {
			if event, hasEvent, err := d.dispatch(); err != nil || hasEvent {
				return event, hasEvent, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, hasValue := strings.Cut(line, ":")
		if hasValue && strings.HasPrefix(value, " ") {
			value = value[1:]
		}
		switch field {
		case "event":
			if err := d.addBufferedBytes(len(value)); err != nil {
				return sseEvent{}, false, err
			}
			d.eventName = value
		case "data":
			if err := d.addBufferedBytes(len(value) + 1); err != nil {
				return sseEvent{}, false, err
			}
			d.dataLines = append(d.dataLines, value)
		}
	}
}

func (d *sseDecoder) readLine() (string, bool, error) {
	var line []byte
	for {
		fragment, err := d.reader.ReadSlice('\n')
		if len(fragment) > 0 {
			if len(line)+len(fragment) > d.maxEventBytes {
				return "", false, fmt.Errorf("%w: provider stream line exceeds %d bytes", ErrValidation, d.maxEventBytes)
			}
			line = append(line, fragment...)
		}
		switch {
		case err == nil:
			return strings.TrimRight(string(line), "\r\n"), true, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if len(line) == 0 {
				return "", false, nil
			}
			return strings.TrimRight(string(line), "\r\n"), true, nil
		case errors.Is(err, io.ErrUnexpectedEOF):
			return "", false, fmt.Errorf("provider stream ended unexpectedly: %w", err)
		case strings.Contains(strings.ToLower(err.Error()), "unexpected eof"):
			return "", false, fmt.Errorf("%w: %v", io.ErrUnexpectedEOF, err)
		default:
			return "", false, err
		}
	}
}

func (d *sseDecoder) dispatch() (sseEvent, bool, error) {
	if len(d.dataLines) == 0 {
		d.reset()
		return sseEvent{}, false, nil
	}
	event := sseEvent{
		Event: d.eventName,
		Data:  strings.Join(d.dataLines, "\n"),
	}
	d.reset()
	return event, true, nil
}

func (d *sseDecoder) reset() {
	d.eventName = ""
	d.dataLines = nil
	d.bufferedBytes = 0
}

func (d *sseDecoder) addBufferedBytes(count int) error {
	d.bufferedBytes += count
	if d.bufferedBytes > d.maxEventBytes {
		return fmt.Errorf("%w: provider stream event exceeds %d bytes", ErrValidation, d.maxEventBytes)
	}
	return nil
}
