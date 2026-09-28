package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"git-task/internal/testrepo"
)

func TestTrackedStorageCaseAliases(t *testing.T) {
	for _, name := range []string{".GIT-TASK", ".Git-Task"} {
		for _, location := range []string{"index", "head"} {
			for _, removed := range []bool{false, true} {
				label := name + "/" + location
				if removed {
					label += "/removed"
				}
				t.Run(label, func(t *testing.T) {
					s, c := initialized(t)
					base := snapshot(t, s)
					next := added(t, base, "must not save")
					alias := filepath.Join(c.Dir, name)
					// Both paths are direct children of this test-owned repository.
					if err := os.Rename(s.dir, alias); err != nil {
						t.Fatal(err)
					}
					a, err := os.Stat(s.dir)
					if errors.Is(err, os.ErrNotExist) {
						t.Skip("filesystem distinguishes case; covered by distinct-directory test")
					}
					if err != nil {
						t.Fatal(err)
					}
					b, err := os.Stat(alias)
					if err != nil {
						t.Fatal(err)
					}
					if !os.SameFile(a, b) {
						t.Fatal("expected the same physical directory")
					}
					testrepo.Run(t, c, "add", "-f", "--", name+"/plan.json")
					if location == "head" {
						testrepo.Commit(t, c)
						testrepo.Run(t, c, "rm", "--cached", "--", name+"/plan.json")
					}
					if removed {
						if err := os.Remove(filepath.Join(alias, "plan.json")); err != nil {
							t.Fatal(err)
						}
						if err := os.Remove(alias); err != nil {
							t.Fatal(err)
						}
					}
					repo, err := c.Discover(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					paths := []string{filepath.Join(s.dir, "plan.json"), s.excludePath, filepath.Join(repo.GitDir, "index"), filepath.Join(repo.GitDir, "HEAD")}
					before := captureFiles(t, paths)
					refs := testrepo.Run(t, c, "for-each-ref", "--format=%(refname) %(objectname)")
					for _, operation := range []struct {
						name string
						run  func() error
					}{
						{"init", func() error { _, err := s.Init(context.Background(), "main"); return err }},
						{"load", func() error { _, err := s.Load(context.Background()); return err }},
						{"save", func() error { _, err := s.Save(context.Background(), base, next); return err }},
					} {
						if err := operation.run(); err == nil {
							t.Fatalf("%s accepted tracked case alias", operation.name)
						}
						if after := captureFiles(t, paths); !reflect.DeepEqual(before, after) {
							t.Fatalf("%s changed files", operation.name)
						}
						if !bytes.Equal(refs, testrepo.Run(t, c, "for-each-ref", "--format=%(refname) %(objectname)")) {
							t.Fatalf("%s changed refs", operation.name)
						}
					}
					entries, err := os.ReadDir(s.dir)
					if removed {
						if !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("refusal created directory: %v", err)
						}
					} else if err != nil || len(entries) != 1 || entries[0].Name() != "plan.json" {
						t.Fatalf("refusal left service files: %v %v", entries, err)
					}
				})
			}
		}
	}
}

func TestTrackedCaseDistinctDirectoriesRemainUsable(t *testing.T) {
	for _, location := range []string{"index", "head"} {
		for _, removed := range []bool{false, true} {
			name := location
			if removed {
				name += "/removed"
			}
			t.Run(name, func(t *testing.T) {
				s, c := initialized(t)
				other := filepath.Join(c.Dir, ".GIT-TASK")
				if err := os.Mkdir(other, 0700); errors.Is(err, os.ErrExist) {
					t.Skip("filesystem aliases case; covered by alias test")
				} else if err != nil {
					t.Fatal(err)
				}
				foreign := filepath.Join(other, "plan.json")
				put(t, foreign, []byte("unrelated tracked data"))
				testrepo.Run(t, c, "add", "-f", "--", ".GIT-TASK/plan.json")
				if location == "head" {
					testrepo.Commit(t, c)
					testrepo.Run(t, c, "rm", "--cached", "--", ".GIT-TASK/plan.json")
				}
				if removed {
					for _, path := range []string{foreign, other, filepath.Join(s.dir, "plan.json"), s.dir} {
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
					}
				}
				before := captureFiles(t, []string{foreign, filepath.Join(c.Dir, ".git", "index"), filepath.Join(c.Dir, ".git", "HEAD")})
				if _, err := s.Init(context.Background(), "main"); err != nil {
					t.Fatal(err)
				}
				base := snapshot(t, s)
				if changed, err := s.Save(context.Background(), base, added(t, base, "allowed")); err != nil || !changed {
					t.Fatalf("distinct path rejected: %v %v", changed, err)
				}
				if after := captureFiles(t, []string{foreign, filepath.Join(c.Dir, ".git", "index"), filepath.Join(c.Dir, ".git", "HEAD")}); !reflect.DeepEqual(before, after) {
					t.Fatal("unrelated tracked data changed")
				}
			})
		}
	}
}

func captureFiles(t *testing.T, paths []string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		result[path] = data
	}
	return result
}
