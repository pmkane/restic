package main

import (
	"context"
	"path"
	"reflect"
	"sort"

	"github.com/restic/restic/internal/debug"
	"github.com/restic/restic/internal/errors"
	"github.com/restic/restic/internal/repository"
	"github.com/restic/restic/internal/restic"
	"github.com/spf13/cobra"
)

var cmdDiff = &cobra.Command{
	Use:   "diff snapshot-ID snapshot-ID",
	Short: "Show differences between two snapshots",
	Long: `
The "diff" command shows differences from the first to the second snapshot.
`,
	DisableAutoGenTag: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDiff(diffOptions, globalOptions, args)
	},
}

// DiffOptions collects all options for the diff command.
type DiffOptions struct{}

var diffOptions DiffOptions

func init() {
	cmdRoot.AddCommand(cmdDiff)
}

func loadSnapshot(ctx context.Context, repo *repository.Repository, desc string) (*restic.Snapshot, error) {
	id, err := restic.FindSnapshot(repo, desc)
	if err != nil {
		return nil, err
	}

	return restic.LoadSnapshot(ctx, repo, id)
}

func diffTree(ctx context.Context, repo *repository.Repository, prefix string, id1, id2 restic.ID) error {
	debug.Log("diffing %v to %v", id1, id2)
	tree1, err := repo.LoadTree(ctx, id1)
	if err != nil {
		return err
	}

	tree2, err := repo.LoadTree(ctx, id2)
	if err != nil {
		return err
	}

	uniqueNames := make(map[string]struct{})
	tree1Nodes := make(map[string]*restic.Node)
	for _, node := range tree1.Nodes {
		tree1Nodes[node.Name] = node
		uniqueNames[node.Name] = struct{}{}
	}
	tree2Nodes := make(map[string]*restic.Node)
	for _, node := range tree2.Nodes {
		tree2Nodes[node.Name] = node
		uniqueNames[node.Name] = struct{}{}
	}

	names := make([]string, 0, len(uniqueNames))
	for name := range uniqueNames {
		names = append(names, name)
	}

	sort.Sort(sort.StringSlice(names))

	for _, name := range names {
		node1, t1 := tree1Nodes[name]
		node2, t2 := tree2Nodes[name]

		switch {
		case t1 && t2:
			name := path.Join(prefix, name)
			mod := ""

			if node1.Type != node2.Type {
				mod += "T"
			}

			if node2.Type == "dir" {
				name += "/"
			}

			if node1.Type == "file" &&
				node2.Type == "file" &&
				!reflect.DeepEqual(node1.Content, node2.Content) {
				mod += "C"
				if !node1.Equals(*node2) {
					mod += "M"
				}
			} else if !node1.Equals(*node2) {
				mod += "M"
			}

			if mod != "" {
				Printf(" % -3v %v\n", mod, name)
			}

			if node1.Type == "dir" && node2.Type == "dir" {
				err := diffTree(ctx, repo, name, *node1.Subtree, *node2.Subtree)
				if err != nil {
					Warnf("error: %v\n", err)
				}
			}
		case t1 && !t2:
			Printf("-    %v\n", path.Join(prefix, name))
		case !t1 && t2:
			Printf("+    %v\n", path.Join(prefix, name))
		}
	}

	return nil
}

func runDiff(opts DiffOptions, gopts GlobalOptions, args []string) error {
	if len(args) != 2 {
		return errors.Fatalf("specify two snapshot IDs")
	}

	ctx, cancel := context.WithCancel(gopts.ctx)
	defer cancel()

	repo, err := OpenRepository(gopts)
	if err != nil {
		return err
	}

	if err = repo.LoadIndex(ctx); err != nil {
		return err
	}

	if !gopts.NoLock {
		lock, err := lockRepo(repo)
		defer unlockRepo(lock)
		if err != nil {
			return err
		}
	}

	sn1, err := loadSnapshot(ctx, repo, args[0])
	if err != nil {
		return err
	}

	sn2, err := loadSnapshot(ctx, repo, args[1])
	if err != nil {
		return err
	}

	Verbosef("comparing snapshot %v to %v:\n", sn1.ID().Str(), sn2.ID().Str())

	if sn1.Tree == nil {
		return errors.Errorf("snapshot %v has nil tree", sn1.ID().Str())
	}

	if sn2.Tree == nil {
		return errors.Errorf("snapshot %v has nil tree", sn2.ID().Str())
	}

	err = diffTree(ctx, repo, "/", *sn1.Tree, *sn2.Tree)
	if err != nil {
		return err
	}

	return nil
}
