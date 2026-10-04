package task

import (
	"fmt"
	"slices"
)

func existingOrder(base, order []string) []string {
	result := []string{}
	for _, id := range order {
		if slices.Contains(base, id) {
			result = append(result, id)
		}
	}
	return result
}

func mergeOrder(base, local, remote []string) ([]string, error) {
	l, r := existingOrder(base, local), existingOrder(base, remote)
	core, err := mergeValue("order", base, l, r)
	if err != nil {
		return nil, err
	}
	result := slices.Clone(core)
	for _, order := range [][]string{remote, local} {
		for i, id := range order {
			if slices.Contains(result, id) {
				continue
			}
			index := 0
			for j := i - 1; j >= 0; j-- {
				if n := slices.Index(result, order[j]); n >= 0 {
					index = n + 1
					break
				}
			}
			result = slices.Insert(result, index, id)
		}
	}
	return result, nil
}

func normalizeShared(p *Plan, remote Plan) error {
	used := map[uint64]bool{}
	var last uint64
	for _, t := range remote.Tasks {
		for _, a := range t.Attempts {
			if a.Completion != nil {
				used[a.Completion.Event] = true
				last = max(last, a.Completion.Event)
			}
		}
	}
	for i := range p.Tasks {
		t := &p.Tasks[i]
		for j := range t.Attempts {
			a := &t.Attempts[j]
			if a.Completion == nil {
				continue
			}
			present := false
			if rt, e := remote.FindID(t.ID); e == nil {
				if ra, e := rt.Attempt(a.ID); e == nil && ra.Completion != nil {
					a.Completion = ra.Completion
					present = true
				}
			}
			if present {
				continue
			}
			c := *a.Completion
			if c.Event == 0 || used[c.Event] {
				last++
				if last == 0 {
					return invalid("исчерпаны номера завершений")
				}
				c.Event = last
			}
			used[c.Event] = true
			last = max(last, c.Event)
			a.Completion = &c
		}
	}
	p.LastEvent = last
	usedNumbers := map[string]bool{}
	for _, t := range remote.Tasks {
		usedNumbers[t.Number] = true
	}
	for i := range p.Tasks {
		t := &p.Tasks[i]
		if _, err := remote.FindID(t.ID); err == nil {
			continue
		}
		if usedNumbers[t.Number] {
			for n := 1; ; n++ {
				number := fmt.Sprintf("T-%03d", n)
				if !usedNumbers[number] {
					t.Number = number
					break
				}
			}
		}
		usedNumbers[t.Number] = true
	}
	return nil
}
