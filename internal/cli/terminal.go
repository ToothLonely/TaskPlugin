package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

func OpenTerminalDialogue(in, out *os.File) (Dialogue, error) {
	if !isTerminal(in) || !isTerminal(out) {
		return nil, fmt.Errorf("--select требует терминал ввода и диагностики; используйте --id, --title или --new")
	}
	return newTerminalDialogue(in, out)
}

type pollingDialogue struct {
	file   *os.File
	reader *bufio.Reader
}

func (d *pollingDialogue) ReadLine(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		d.file.SetReadDeadline(time.Now())
		close(done)
	})
	line, err := d.reader.ReadString('\n')
	if !stop() {
		<-done
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return line, err
}

func (d *pollingDialogue) Close() error { return d.file.Close() }

func openPollingDialogue(file *os.File) (Dialogue, error) {
	if err := file.SetReadDeadline(time.Time{}); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return &pollingDialogue{file: file, reader: bufio.NewReader(file)}, nil
}
