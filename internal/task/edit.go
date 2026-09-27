package task

// EditOptions distinguishes omitted fields (nil) from supplied empty text.
type EditOptions struct {
	Title       *string
	Description *string
}

// Edit updates supplied text fields in any state without changing task history.
// Identical values and an empty options value are no-ops.
func (p *Plan) Edit(id string, opts EditOptions) (bool, error) {
	return p.change(id, func(_ *Plan, t *Task) (bool, error) {
		title, description := t.Title, t.Description
		if opts.Title != nil {
			title = *opts.Title
		}
		if opts.Description != nil {
			description = *opts.Description
		}
		if title == t.Title && description == t.Description {
			return false, nil
		}
		t.Title, t.Description = title, description
		return true, nil
	})
}
