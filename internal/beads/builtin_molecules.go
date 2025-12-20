// Package beads provides a wrapper for the bd (beads) CLI.
package beads

// BuiltinMolecule defines a built-in molecule template.
type BuiltinMolecule struct {
	ID          string // Well-known ID (e.g., "mol-engineer-in-box")
	Title       string
	Description string
}

// BuiltinMolecules returns all built-in molecule definitions.
func BuiltinMolecules() []BuiltinMolecule {
	return []BuiltinMolecule{
		EngineerInBoxMolecule(),
		QuickFixMolecule(),
		ResearchMolecule(),
		InstallGoBinaryMolecule(),
	}
}

// EngineerInBoxMolecule returns the engineer-in-box molecule definition.
// This is a full workflow from design to merge.
func EngineerInBoxMolecule() BuiltinMolecule {
	return BuiltinMolecule{
		ID:    "mol-engineer-in-box",
		Title: "Engineer in a Box",
		Description: `Full workflow from design to merge.

## Step: design
Think carefully about architecture. Consider:
- Existing patterns in the codebase
- Trade-offs between approaches
- Testability and maintainability

Write a brief design summary before proceeding.

## Step: implement
Write the code. Follow codebase conventions.
Needs: design

## Step: review
Self-review the changes. Look for:
- Bugs and edge cases
- Style issues
- Missing error handling
Needs: implement

## Step: test
Write and run tests. Cover happy path and edge cases.
Fix any failures before proceeding.
Needs: implement

## Step: submit
Submit for merge via refinery.
Needs: review, test`,
	}
}

// QuickFixMolecule returns the quick-fix molecule definition.
// This is a fast path for small changes.
func QuickFixMolecule() BuiltinMolecule {
	return BuiltinMolecule{
		ID:    "mol-quick-fix",
		Title: "Quick Fix",
		Description: `Fast path for small changes.

## Step: implement
Make the fix. Keep it focused.

## Step: test
Run relevant tests. Fix any regressions.
Needs: implement

## Step: submit
Submit for merge.
Needs: test`,
	}
}

// ResearchMolecule returns the research molecule definition.
// This is an investigation workflow.
func ResearchMolecule() BuiltinMolecule {
	return BuiltinMolecule{
		ID:    "mol-research",
		Title: "Research",
		Description: `Investigation workflow.

## Step: investigate
Explore the question. Search code, read docs,
understand context. Take notes.

## Step: document
Write up findings. Include:
- What you learned
- Recommendations
- Open questions
Needs: investigate`,
	}
}

// InstallGoBinaryMolecule returns the install-go-binary molecule definition.
// This is a single step to rebuild and install the gt binary after code changes.
func InstallGoBinaryMolecule() BuiltinMolecule {
	return BuiltinMolecule{
		ID:    "mol-install-go-binary",
		Title: "Install Go Binary",
		Description: `Single step to rebuild and install the gt binary after code changes.

## Step: install
Build and install the gt binary locally.

Run from the rig directory:
` + "```" + `
go build -o gt ./cmd/gt
go install ./cmd/gt
` + "```" + `

Verify the installed binary is updated:
` + "```" + `
which gt
gt --version  # if version command exists
` + "```",
	}
}

// SeedBuiltinMolecules creates all built-in molecules in the beads database.
// It skips molecules that already exist (by title match).
// Returns the number of molecules created.
func (b *Beads) SeedBuiltinMolecules() (int, error) {
	molecules := BuiltinMolecules()
	created := 0

	// Get existing molecules to avoid duplicates
	existing, err := b.List(ListOptions{Type: "molecule", Priority: -1})
	if err != nil {
		return 0, err
	}

	// Build map of existing molecule titles
	existingTitles := make(map[string]bool)
	for _, issue := range existing {
		existingTitles[issue.Title] = true
	}

	// Create each molecule if it doesn't exist
	for _, mol := range molecules {
		if existingTitles[mol.Title] {
			continue // Already exists
		}

		_, err := b.Create(CreateOptions{
			Title:       mol.Title,
			Type:        "molecule",
			Priority:    2, // Medium priority
			Description: mol.Description,
		})
		if err != nil {
			return created, err
		}
		created++
	}

	return created, nil
}
