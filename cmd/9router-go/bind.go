package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/urfave/cli/v2"

	"9router/proxy/internal/db"
	"9router/proxy/internal/modelalias"
	"9router/proxy/internal/persona"
)

// bindModelCommand creates the persona-bound model name — the same shape as a
// "-mod" variant on the providers this gateway proxies to.
//
// One command does three things that must agree or the result silently
// under-delivers: it stores the persona (from a file or inline text), stores the
// binding that attaches it to a model name, and reports exactly what will happen
// on the next request. Splitting them into separate commands would leave a
// window where a binding names a persona that was never stored — which resolves
// fail-closed, so the operator would see a rejected request rather than the
// persona they thought they had installed.
func bindModelCommand() *cli.Command {
	return &cli.Command{
		Name:      "bind",
		Usage:     "Bind a model name to a target model and/or a persona file",
		ArgsUsage: "<model-name>",
		Description: `Creates a model name that carries prompt context, so a client that cannot send
a system prompt can select a persona by model id alone.

Flags must come BEFORE the model name: the CLI stops parsing options at the first
positional argument, so a flag written after the name would be ignored.

Examples:
  # Persona from a markdown file, attached to an existing catalog model
  9router bind --persona-file ./ltx-quasar.md --persona-id ltx-quasar ltx-mod

  # Rename and attach in one binding
  9router bind --target shiteru/glm-5.3 --persona-file ./persona.md --persona-id ltx ltx-mod

  # Persona already stored; just attach it to a name
  9router bind --persona-id ltx-quasar ltx-mod

  # List what is bound
  9router bind --list

  # Turn a binding off without deleting it
  9router bind --disable ltx-mod

Every request naming the bound model pays the persona's token cost, exactly like
the -mod variants this mirrors: the text is prepended on every call.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "target", Usage: "model this name resolves to (omit to keep the name as-is)"},
			&cli.StringFlag{Name: "persona-id", Usage: "persona id to attach (defaults to the bound name; omit to keep the existing one)"},
			&cli.BoolFlag{Name: "clear-persona", Usage: "remove the persona from an existing binding"},
			&cli.BoolFlag{Name: "clear-target", Usage: "remove the model rewrite from an existing binding"},
			&cli.StringFlag{Name: "persona-file", Usage: "path to a file whose contents become the persona text"},
			&cli.StringFlag{Name: "persona-text", Usage: "persona text inline (use --persona-file for long personas)"},
			&cli.BoolFlag{Name: "replace", Usage: "replace the caller's system prompt instead of appending below it"},
			&cli.BoolFlag{Name: "disable", Usage: "disable an existing binding instead of creating one"},
			&cli.BoolFlag{Name: "list", Usage: "list bindings and exit"},
			&cli.BoolFlag{Name: "delete", Usage: "delete the binding named by the argument"},
		},
		Action: runBind,
	}
}

func runBind(cCtx *cli.Context) error {
	conn, err := openDBForCommand()
	if err != nil {
		return err
	}
	defer conn.Close()
	repo := db.NewRepo(conn)

	if cCtx.Bool("list") {
		return listBindings(repo)
	}

	// A flag written after the model name is not parsed as a flag, so it arrives
	// here as an extra argument. Failing loudly beats creating a binding that
	// silently ignores every option the operator typed.
	if extra := cCtx.Args().Slice(); len(extra) > 1 {
		return cli.Exit(fmt.Sprintf(
			"unexpected arguments after the model name: %v\n"+
				"Flags must come before the model name, for example:\n"+
				"  9router bind --target shiteru/glm-5.3 --persona-file ./persona.md --persona-id ltx ltx-mod",
			extra[1:]), 2)
	}

	name := strings.TrimSpace(cCtx.Args().First())
	if name == "" {
		return cli.Exit("a model name is required: 9router bind --persona-file ./persona.md ltx-mod", 2)
	}
	if !modelalias.ValidID(name) {
		return cli.Exit(fmt.Sprintf("invalid model name %q: use letters, digits, dot, dash, underscore or slash", name), 2)
	}

	if cCtx.Bool("delete") {
		if err := repo.DeleteModelBinding(name); err != nil {
			return err
		}
		fmt.Printf("Deleted binding %q. Requests naming it are now unknown models unless a catalog or alias resolves them.\n", name)
		return nil
	}

	personaID := strings.TrimSpace(cCtx.String("persona-id"))
	if personaID == "" {
		personaID = name
	}

	// Store the persona first when text or a file was supplied, so the binding
	// created below can never reference a persona that is not there yet.
	file := strings.TrimSpace(cCtx.String("persona-file"))
	inline := cCtx.String("persona-text")
	if file != "" && inline != "" {
		return cli.Exit("use either --persona-file or --persona-text, not both", 2)
	}
	if file != "" || inline != "" {
		text := inline
		if file != "" {
			raw, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("read persona file: %w", err)
			}
			text = string(raw)
		}
		p := persona.Persona{
			ID:             personaID,
			SystemPrompt:   text,
			AppendExisting: !cCtx.Bool("replace"),
		}
		if err := repo.SetPersona(p); err != nil {
			return fmt.Errorf("store persona %q: %w", personaID, err)
		}
		mode := "appended below any caller system prompt"
		if cCtx.Bool("replace") {
			mode = "REPLACING the caller system prompt"
		}
		fmt.Printf("Stored persona %q (%d bytes, %s).\n", personaID, len(text), mode)
		if file != "" {
			fmt.Printf("  source: %s\n", filepath.Clean(file))
		}
	}

	// An existing persona is required when the binding attaches one, so a typo in
	// the id is reported now rather than as a rejected request later.
	//
	// An update that names no persona keeps the one already bound. Without this,
	// a plain `bind --target X <name>` would silently drop the persona, turning a
	// rename into a prompt-context removal — the opposite of what was asked.
	// `--clear-persona` is how an operator removes it on purpose.
	attached := ""
	personaNamed := cCtx.String("persona-id") != "" || file != "" || inline != "" || cCtx.Bool("clear-persona")
	if existing, err := repo.GetModelBinding(name); err == nil && existing != nil && !personaNamed {
		attached = existing.Persona
	}
	if cCtx.Bool("clear-persona") {
		attached = ""
	}
	if stored, err := repo.GetPersona(personaID); err != nil {
		return err
	} else if stored != nil {
		attached = personaID
	} else if file != "" || inline != "" {
		return fmt.Errorf("persona %q was not stored", personaID)
	} else if attached == "" {
		fmt.Printf("Note: no persona %q is stored, so this binding carries no prompt context.\n", personaID)
		fmt.Printf("      Attach one with: 9router bind --persona-file ./persona.md --persona-id %s %s\n", personaID, name)
	}

	// Each field is preserved unless the operator named it, so an update that
	// changes one thing cannot silently discard the other. `--clear-target` and
	// `--clear-persona` are the explicit ways to remove one.
	target := strings.TrimSpace(cCtx.String("target"))
	if target == "" && !cCtx.Bool("clear-target") {
		if existing, err := repo.GetModelBinding(name); err == nil && existing != nil {
			target = existing.Target
		}
	}
	if cCtx.Bool("clear-target") {
		target = ""
	}
	binding := modelalias.Binding{
		ID:      name,
		Target:  target,
		Persona: attached,
		Enabled: !cCtx.Bool("disable"),
	}
	if err := repo.SetModelBinding(binding); err != nil {
		return fmt.Errorf("store binding %q: %w", name, err)
	}

	if !binding.Enabled {
		fmt.Printf("Binding %q is DISABLED: requests naming it fail as unknown models.\n", name)
		return nil
	}
	effect := "keeps the model name as sent"
	if binding.Target != "" {
		effect = fmt.Sprintf("resolves to %q", binding.Target)
	}
	personaNote := "no persona attached"
	if binding.Persona != "" {
		personaNote = fmt.Sprintf("attaches persona %q on every call", binding.Persona)
	}
	fmt.Printf("Bound %q: %s, %s.\n", name, effect, personaNote)
	fmt.Printf("Use it as the model name:  \"model\": %q\n", name)
	return nil
}

// listBindings renders every binding with its effective behavior, so the
// operator can see what a name does without reading the settings row.
func listBindings(repo *db.Repo) error {
	settings, err := repo.GetSettings()
	if err != nil {
		return err
	}
	if len(settings.ModelBindings) == 0 {
		fmt.Println("No model bindings configured.")
		fmt.Println("Create one with: 9router bind <name> --target <model> --persona-file ./persona.md")
		return nil
	}
	names := make([]string, 0, len(settings.ModelBindings))
	for name := range settings.ModelBindings {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("%-24s %-6s %-30s %s\n", "NAME", "STATE", "TARGET", "PERSONA")
	for _, name := range names {
		b := settings.ModelBindings[name]
		state := "on"
		if !b.Enabled {
			state = "off"
		}
		target := b.Target
		if target == "" {
			target = "(unchanged)"
		}
		personaID := b.Persona
		if personaID == "" {
			personaID = "(none)"
		}
		fmt.Printf("%-24s %-6s %-30s %s\n", name, state, target, personaID)
	}
	return nil
}
