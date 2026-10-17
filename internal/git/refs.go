package git

import (
	"context"
	"errors"
	"strings"

	"git-task/internal/diagnostic"
)

// ErrInvalidBranch indicates an invalid short local branch name.
var ErrInvalidBranch = diagnostic.New("недопустимое имя локальной ветки")

// BranchRef validates a literal short branch name and returns its full ref.
// Using a full ref avoids check-ref-format --branch expanding @{-1} from HEAD.
func (c *Client) BranchRef(ctx context.Context, name string) (string, error) {
	if name == "" || name == "HEAD" || strings.ContainsRune(name, '\x00') || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "refs/") {
		return "", diagnostic.Errorf("%w: %q", ErrInvalidBranch, name)
	}
	ref := "refs/heads/" + name
	_, err := c.Run(ctx, "check-ref-format", ref)
	var command *CommandError
	if errors.As(err, &command) && command.Result.ExitCode == 1 {
		return "", diagnostic.Errorf("%w: %q", ErrInvalidBranch, name)
	}
	if err != nil {
		return "", err
	}
	return ref, nil
}
