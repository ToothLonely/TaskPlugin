package git

import (
	"context"
	"fmt"
	"strings"
)

func (c *Client) Editor(ctx context.Context) (string, error) {
	value := func(name string) string {
		env := c.environment()
		for i := len(env) - 1; i >= 0; i-- {
			entry := env[i]
			key, text, _ := strings.Cut(entry, "=")
			if strings.EqualFold(key, name) {
				return text
			}
		}
		return ""
	}
	if editor := value("GIT_EDITOR"); strings.TrimSpace(editor) != "" {
		return editor, nil
	}
	r, err := c.Run(ctx, "config", "--get", "core.editor")
	if err != nil && !exitCode(err, 1) {
		return "", err
	}
	if editor := strings.TrimSpace(string(r.Stdout)); editor != "" {
		return editor, nil
	}
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if editor := value(name); strings.TrimSpace(editor) != "" {
			return editor, nil
		}
	}
	return "", fmt.Errorf("редактор не настроен; используйте edit --title или --description")
}
