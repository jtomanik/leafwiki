package tree

import (
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("move node page moves file strict", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		secA := &PageNode{ID: "a", Slug: "a", Title: "A", Kind: NodeKindSection, Parent: root}
		secB := &PageNode{ID: "b", Slug: "b", Title: "B", Kind: NodeKindSection, Parent: root}
		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: secA}

		// create source file at old location (tree-based path)
		src := filepath.Join(tmp, "root", "a", "p.md")
		writeTreeFile(src, "# hi", 0o644)
		{

			err := store.MoveNode(page, secB)
			Expect(err).To(Succeed(), "MoveNode: %v",

				err)
		}

		dst := filepath.Join(tmp, "root", "b", "p.md")
		{
			_, err := os.Stat(dst)
			Expect(err).To(Succeed(), "expected dest file: %v",

				err,
			)
		}
		{

			_, err := os.Stat(src)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected src removed",
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("move node page uses workspace source path", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		plans := &PageNode{ID: "plans", Slug: "plans", Title: "Plans", Kind: NodeKindSection, Parent: root}
		archive := &PageNode{ID: "archive", Slug: "archive", Title: "Archive", Kind: NodeKindSection, Parent: root}
		page := &PageNode{
			ID:                  "p1",
			Slug:                "agent-hooks-plan",
			Title:               "Agent Hooks Plan",
			Kind:                NodeKindPage,
			Parent:              plans,
			WorkspaceSourcePath: "plans/agent_hooks.PLAN.md",
		}

		src := filepath.Join(tmp, "root", "plans", "agent_hooks.PLAN.md")
		writeTreeFile(src, "# plan", 0o644)
		{

			err := store.MoveNode(page, archive)
			Expect(err).To(Succeed(), "MoveNode: %v",

				err)
		}

		dst := filepath.Join(tmp, "root", "archive", "agent_hooks.PLAN.md")
		{
			_, err := os.Stat(dst)
			Expect(err).To(Succeed(), "expected retained source filename at destination: %v",

				err,
			)
		}
		{

			_, err := os.Stat(src)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected original source file removed",
			)
		}
		Expect(page.WorkspaceSourcePath).To(
			Equal(newFixtureWorkspaceSourcePath("archive/agent_hooks.PLAN.md")), "WorkspaceSourcePath = %q, want archive/agent_hooks.PLAN.md",

			page.WorkspaceSourcePath)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("move node drift when missing source", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: "s", Slug: "s", Title: "S", Kind: NodeKindSection, Parent: root}
		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: sec}

		err := store.MoveNode(page, root)

		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("create page uses workspace source parent directory", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		parent := &PageNode{
			ID:                  "s1",
			Slug:                "my-docs",
			Title:               "My Docs",
			Kind:                NodeKindSection,
			Parent:              root,
			WorkspaceSourcePath: "My Docs",
		}
		page := &PageNode{ID: "p1", Slug: "child", Title: "Child", Kind: NodeKindPage, Parent: parent}
		{

			err := store.CreatePage(parent, page)
			Expect(err).To(Succeed(), "CreatePage: %v",

				err)
		}

		want := filepath.Join(tmp, "root", "My Docs", "child.md")
		{
			_, err := os.Stat(want)
			Expect(err).To(Succeed(), "expected page under retained parent source directory: %v",

				err)
		}
		Expect(page.WorkspaceSourcePath).To(
			Equal(newFixtureWorkspaceSourcePath("My Docs/child.md")), "WorkspaceSourcePath = %q, want My Docs/child.md",

			page.WorkspaceSourcePath,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("delete page removes file or drift if missing", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: root}

		path := filepath.Join(tmp, "root", "p.md")
		writeTreeFile(path, "# x", 0o644)
		{

			err := store.DeletePage(page)
			Expect(err).To(Succeed(), "DeletePage: %v",

				err)
		}
		{

			_, err := os.Stat(path)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected file deleted",
			)
		}

		// delete again -> drift
		err := store.DeletePage(page)
		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v", err, err)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("delete page uses workspace source path", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		plans := &PageNode{ID: "plans", Slug: "plans", Title: "Plans", Kind: NodeKindSection, Parent: root}
		page := &PageNode{
			ID:                  "p1",
			Slug:                "agent-hooks-plan",
			Title:               "Agent Hooks Plan",
			Kind:                NodeKindPage,
			Parent:              plans,
			WorkspaceSourcePath: "plans/agent_hooks.PLAN.md",
		}

		rawSource := filepath.Join(tmp, "root", "plans", "agent_hooks.PLAN.md")
		writeTreeFile(rawSource, "# plan", 0o644)
		{

			err := store.DeletePage(page)
			Expect(err).To(Succeed(), "DeletePage: %v",

				err)
		}
		{

			_, err := os.Stat(rawSource)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected raw source file deleted",
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("delete section removes folder recursive or drift if missing", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}

		directory := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(directory)
		writeTreeFile(filepath.Join(directory, "index.md"), "# hi", 0o644)
		writeTreeFile(filepath.Join(directory, "nested.txt"), "x", 0o644)
		{

			err := store.DeleteSection(sec)
			Expect(err).To(Succeed(), "DeleteSection: %v",

				err)
		}
		{

			_, err := os.Stat(directory)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected folder deleted",
			)
		}

		err := store.DeleteSection(sec)
		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v", err, err)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("save child order uses workspace source section directory", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		parent := &PageNode{
			ID:                  "s1",
			Slug:                "my-docs",
			Title:               "My Docs",
			Kind:                NodeKindSection,
			Parent:              root,
			WorkspaceSourcePath: "My Docs",
			Children: []*PageNode{
				{ID: "p1", Slug: "child", Title: "Child", Kind: NodeKindPage},
			},
		}
		assignParentToChildren(parent)
		{

			err := store.SaveChildOrder(parent)
			Expect(err).To(Succeed(), "SaveChildOrder: %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "My Docs", orderFilename))
			Expect(err).To(Succeed(), "expected child order under retained source directory: %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "my-docs", orderFilename))
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected no child order file under normalized route directory",
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("rename node page and section", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}

		// page rename
		page := &PageNode{ID: "p1", Slug: "old", Title: "P", Kind: NodeKindPage, Parent: root}
		oldFile := filepath.Join(tmp, "root", "old.md")
		writeTreeFile(oldFile, "# x", 0o644)
		{

			err := store.RenameNode(page, "new")
			Expect(err).To(Succeed(), "RenameNode(page): %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "new.md"))
			Expect(err).To(Succeed(), "expected new page file")
		}

		// section rename
		sec := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}
		secDir := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(secDir)
		writeTreeFile(filepath.Join(secDir, "index.md"), "# y", 0o644)
		{

			err := store.RenameNode(sec, "docs2")
			Expect(err).To(Succeed(), "RenameNode(section): %v",

				err,
			)
		}
		Expect(filepath.Join(tmp, "root", "docs2")).To(BeADirectory(), "expected renamed section directory")

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("rename node page uses workspace source path and clears default source", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		plans := &PageNode{ID: "plans", Slug: "plans", Title: "Plans", Kind: NodeKindSection, Parent: root}
		page := &PageNode{
			ID:                  "p1",
			Slug:                "agent-hooks-plan",
			Title:               "Agent Hooks Plan",
			Kind:                NodeKindPage,
			Parent:              plans,
			WorkspaceSourcePath: "plans/agent_hooks.PLAN.md",
		}
		rawSource := filepath.Join(tmp, "root", "plans", "agent_hooks.PLAN.md")
		writeTreeFile(rawSource, "# plan", 0o644)
		{

			err := store.RenameNode(page, "agent-hooks-v2")
			Expect(err).To(Succeed(), "RenameNode: %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "plans", "agent-hooks-v2.md"))
			Expect(err).To(Succeed(), "expected renamed canonical page file: %v",

				err)
		}
		{

			_, err := os.Stat(rawSource)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected raw source filename removed",
			)
		}
		Expect(page.WorkspaceSourcePath).To(
			BeEmpty(), "WorkspaceSourcePath = %q, want cleared default source",

			page.WorkspaceSourcePath)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("rename node rejects empty slug and root", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "old", Title: "P", Kind: NodeKindPage, Parent: root}
		{

			err := store.RenameNode(page, "   ")
			Expect(err).To(MatchError(ErrInvalidOperation), "expected empty slug validation error, got %v", err)
		}
		{

			err := store.RenameNode(root, "new-root")
			Expect(err).To(MatchError(ErrInvalidOperation), "expected root rename validation error, got %v", err)
		}

	})
})
