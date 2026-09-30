package git

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var ErrHistoryUnavailable = errors.New("недостаточно локальной истории или превышен бюджет сверки")

const historyLimit = 4096

type RefUpdate struct {
	Old     string
	New     string
	Message string
	Digest  string
}

type RefLog struct {
	Entries []RefUpdate
}

func (l RefLog) Digest() string {
	if len(l.Entries) == 0 {
		return ""
	}
	return l.Entries[len(l.Entries)-1].Digest
}

func (c *Client) BranchLog(ctx context.Context, branch string) (RefLog, error) {
	ref, err := c.BranchRef(ctx, branch)
	if err != nil {
		return RefLog{}, err
	}
	r, err := c.Run(ctx, "rev-parse", "--path-format=absolute", "--git-path", "logs/"+ref)
	if err != nil {
		return RefLog{}, err
	}
	f, err := os.Open(outputLine(r.Stdout))
	if errors.Is(err, os.ErrNotExist) {
		return RefLog{}, nil
	}
	if err != nil {
		return RefLog{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, 4*1024*1024+1))
	if err = errors.Join(readErr, f.Close()); err != nil {
		return RefLog{}, err
	}
	if len(data) > 4*1024*1024 || len(data) != 0 && data[len(data)-1] != '\n' {
		return RefLog{}, ErrHistoryUnavailable
	}
	var log RefLog
	hash := sha256.New()
	for _, line := range bytes.SplitAfter(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if len(log.Entries) == historyLimit {
			return RefLog{}, ErrHistoryUnavailable
		}
		header, message, found := strings.Cut(strings.TrimSuffix(string(line), "\n"), "\t")
		fields := strings.Fields(header)
		if !found || len(fields) < 3 || !fullOID(fields[0], true) || !fullOID(fields[1], false) {
			return RefLog{}, ErrHistoryUnavailable
		}
		hash.Write(line)
		log.Entries = append(log.Entries, RefUpdate{Old: fields[0], New: fields[1], Message: message, Digest: hex.EncodeToString(hash.Sum(nil))})
	}
	return log, ctx.Err()
}

func fullOID(s string, zero bool) bool {
	return (len(s) == 40 || len(s) == 64) && hexID(s) && (zero || strings.Trim(s, "0") != "")
}

func (c *Client) HasObject(ctx context.Context, oid string) (bool, error) {
	if !fullOID(oid, false) {
		return false, fmt.Errorf("ожидается полный commit ID")
	}
	_, err := c.Run(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", oid+"^{commit}")
	if exitCode(err, 1) {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) IsAncestor(ctx context.Context, ancestor, commit string) (bool, error) {
	if !fullOID(ancestor, false) || !fullOID(commit, false) {
		return false, fmt.Errorf("ожидается полный commit ID")
	}
	_, err := c.Run(ctx, "merge-base", "--is-ancestor", ancestor, commit)
	if exitCode(err, 1) {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) OwnWork(ctx context.Context, tip, target, base string) (string, error) {
	for _, oid := range []string{tip, target, base} {
		if !fullOID(oid, false) {
			return "", fmt.Errorf("ожидается полный commit ID")
		}
	}
	r, err := c.Run(ctx, "rev-list", "--no-merges", "--max-count=1", tip, "^"+target, "^"+base, "--")
	return outputLine(r.Stdout), err
}

func (c *Client) FirstParentRange(ctx context.Context, target, base string) ([]string, error) {
	if !fullOID(target, false) || !fullOID(base, false) {
		return nil, fmt.Errorf("ожидается полный commit ID")
	}
	r, err := c.Run(ctx, "rev-list", "--first-parent", "--max-count=4097", target, "^"+base, "--")
	if err != nil {
		return nil, err
	}
	commits := strings.Fields(string(r.Stdout))
	if len(commits) > historyLimit {
		return nil, ErrHistoryUnavailable
	}
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, nil
}

func (c *Client) Parents(ctx context.Context, commit string) ([]string, error) {
	if !fullOID(commit, false) {
		return nil, fmt.Errorf("ожидается полный commit ID")
	}
	r, err := c.Run(ctx, "rev-list", "--parents", "--max-count=1", commit, "--")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(r.Stdout))
	if len(fields) == 0 || fields[0] != commit {
		return nil, ErrHistoryUnavailable
	}
	return fields[1:], nil
}
