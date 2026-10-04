package git

import (
	"context"
	"strings"
)

func (c *Client) Author(ctx context.Context) (string, error) {
	r, err := c.Run(ctx, "var", "GIT_AUTHOR_IDENT")
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(r.Stdout))
	end := strings.LastIndex(value, ">")
	if end >= 0 {
		value = value[:end+1]
	}
	return value, nil
}
