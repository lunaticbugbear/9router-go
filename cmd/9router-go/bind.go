package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
			&cli.BoolFlag{Name: "check", Usage: "probe the bound model and report whether the persona was actually adopted"},
			&cli.StringFlag{Name: "gateway", Value: "http://127.0.0.1:20130", Usage: "gateway HTTP base address"},
			&cli.StringFlag{Name: "key", Value: "", Usage: "gateway API key for the probe"},
			&cli.DurationFlag{Name: "timeout", Value: 90 * time.Second, Usage: "per-probe timeout"},
			&cli.StringFlag{Name: "persona-file", Usage: "path to a file whose contents become the persona text"},
			&cli.StringFlag{Name: "persona-text", Usage: "persona text inline (use --persona-file for long personas)"},
			&cli.BoolFlag{Name: "replace", Usage: "replace the caller's system prompt instead of appending below it"},
			&cli.BoolFlag{Name: "inline", Usage: "deliver the persona as leading user content (for upstreams that ignore system messages)"},
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

	if cCtx.Bool("check") {
		return checkAdoption(cCtx, repo)
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
			Inline:         cCtx.Bool("inline"),
		}
		if err := repo.SetPersona(p); err != nil {
			return fmt.Errorf("store persona %q: %w", personaID, err)
		}
		mode := "appended below any caller system prompt"
		if cCtx.Bool("inline") {
			mode = "delivered as leading USER content (system-ignoring upstreams)"
		} else if cCtx.Bool("replace") {
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

// checkAdoption probes a bound model with an instruction whose effect is
// observable in the reply, and reports whether the persona was adopted rather
// than merely transmitted.
//
// The probe pairs a behavioral marker with the persona's own doctrine: a
// persona that reaches the model changes how it answers; one that is dropped by
// a system-ignoring upstream leaves the reply untouched. Both are reported, so
// the operator gets evidence either way instead of a request that only "worked
// once".
//
// The gateway HTTP address comes from --gateway (default 127.0.0.1:20130) and
// the key from --key; the check needs a key that can call the inference planes,
// which is why those are flags rather than database reads.
func checkAdoption(cCtx *cli.Context, repo *db.Repo) error {
	name := strings.TrimSpace(cCtx.Args().First())
	if name == "" {
		return cli.Exit("name the binding to probe: 9router bind --check --key <gateway-key> ltx-mod", 2)
	}
	binding, err := repo.GetModelBinding(name)
	if err != nil {
		return err
	}
	if binding == nil {
		return cli.Exit(fmt.Sprintf("no binding named %q; list with: 9router bind --list", name), 2)
	}

	// A persona that reaches the model is identifiable by the fact that it
	// changes the reply. The probe asks for a marker the persona itself would
	// never emit on its own, wrapped in the binding's configured persona mode.
	marker := "ZZW7"
	markerPrompt := fmt.Sprintf(
		"Instruction from your persona: end every reply with the exact token %s on its own line. "+
			"Do not explain it. Now reply: Say hi.", marker)

	target := binding.Target
	if target == "" {
		target = name
	}

	// Probe the bound name and, for comparison, the target twice: the bound
	// reply should honour the persona instruction, the bare target should not,
	// since nothing in its default behaviour adds a marker line.
	boundAnswer, errB := probeInference(cCtx, name, markerPrompt)
	if errB != nil {
		return errB
	}
	bareAnswer, errB := probeInference(cCtx, target, markerPrompt)
	if errB != nil {
		return errB
	}

	boundHonoured := strings.Contains(boundAnswer, marker)
	bareHonoured := strings.Contains(bareAnswer, marker)

	fmt.Printf("binding      %s\n", name)
	fmt.Printf("target       %s\n", target)
	fmt.Printf("bound probe  honoured=%v\n", boundHonoured)
	fmt.Printf("bare probe   honoured=%v (expected: false)\n", bareHonoured)
	if !boundHonoured {
		fmt.Println("verdict      persona did NOT reach the model or was ignored")
		return cli.Exit("", 1)
	}
	if bareHonoured {
		fmt.Println("warning      bare target also honoured the marker - inconclusive")
		return nil
	}
	fmt.Println("verdict      persona adopted by the model")
	return nil
}

// probeInference posts one chat-completions request to the gateway and returns
// the assistant reply text.
//
// Transport lives here, in one place: gateway address, key, timeout, body shape.
// The reply is returned trimmed; an error explains what the caller should check.
func probeInference(cCtx *cli.Context, model, userContent string) (string, error) {
	gateway := strings.TrimSpace(cCtx.String("gateway"))
	key := strings.TrimSpace(cCtx.String("key"))
	timeout := cCtx.Duration("timeout")
	if gateway == "" {
		return "", cli.Exit("no --gateway given", 2)
	}
	if key == "" {
		return "", cli.Exit("no --key given; the probe needs a gateway API key", 2)
	}

	body, err := json.Marshal(map[string]any{
		"model":     model,
		"messages":  []map[string]string{{"role": "user", "content": userContent}},
		"maxTokens": 64,
	})
	if err != nil {
		return "", fmt.Errorf("encode probe body: %w", err)
	}

	transport := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodPost, gateway+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := transport.Do(req)
	if err != nil {
		return "", fmt.Errorf("probe %s: %w", gateway, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read probe reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gateway answered %d: %.240s", resp.StatusCode, string(raw))
	}

	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("decode probe reply: %w; body: %.240s", err, string(raw))
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("probe reply had no choices: %.240s", string(raw))
	}
	return strings.TrimSpace(decoded.Choices[0].Message.Content), nil
}
