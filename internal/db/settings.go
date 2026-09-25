package db

import (
	json "encoding/json/v2"
	"fmt"
	"sort"
	"strings"

	"9router/proxy/internal/bounty"
	"9router/proxy/internal/handlerutil"
	"9router/proxy/internal/modelalias"
	"9router/proxy/internal/persona"
)

// ComboStrategy defines routing strategy, sticky limit, and judge model for a combo.
type ComboStrategy struct {
	Strategy    string `json:"strategy,omitempty"`
	StickyLimit int    `json:"stickyLimit,omitempty"`
	JudgeModel  string `json:"judgeModel,omitempty"`
}

// ProviderStrategy defines routing and proxy pool options for a specific provider.
type ProviderStrategy struct {
	ProxyPoolID           string `json:"proxyPoolId,omitempty"`
	RotateStrategy        string `json:"rotateStrategy,omitempty"` // "none", "round-robin", "random", "sticky"
	StickyLimit           int    `json:"stickyLimit,omitempty"`
	StrictModelAssignment bool   `json:"strictModelAssignment,omitempty"`
}

// CapacityAdapterEntry defines settings for an input-modality capability adapter pool.
type CapacityAdapterEntry struct {
	Enabled    bool     `json:"enabled"`
	RoundRobin bool     `json:"roundRobin"`
	Models     []string `json:"models"`
}

// SettingsData represents token saver, combo routing, and general settings stored in the settings table.
type SettingsData struct {
	RTKEnabled                 bool                            `json:"rtkEnabled"`
	CavemanEnabled             bool                            `json:"cavemanEnabled"`
	CavemanLevel               string                          `json:"cavemanLevel"`
	PonytailEnabled            bool                            `json:"ponytailEnabled"`
	PonytailLevel              string                          `json:"ponytailLevel"`
	HeadroomUrl                string                          `json:"headroomUrl"`
	HeadroomCodeAware          bool                            `json:"headroomCodeAware"`
	HeadroomKompress           bool                            `json:"headroomKompress"`
	HeadroomTimeoutMs          int                             `json:"headroomTimeoutMs"`
	AutoUpdate                 bool                            `json:"autoUpdate"`
	FallbackStrategy           string                          `json:"fallbackStrategy,omitempty"`
	StickyRoundRobinLimit      int                             `json:"stickyRoundRobinLimit,omitempty"`
	ComboStrategy              string                          `json:"comboStrategy,omitempty"`
	ComboStickyRoundRobinLimit int                             `json:"comboStickyRoundRobinLimit,omitempty"`
	ComboStrategies            map[string]ComboStrategy        `json:"comboStrategies,omitempty"`
	ProviderStrategies         map[string]ProviderStrategy     `json:"providerStrategies,omitempty"`
	CapacityAdapter            map[string]CapacityAdapterEntry `json:"capacityAdapter,omitempty"`
	// BountyProfiles store operator-declared authorization scopes. Request and response contents are not stored here.
	BountyProfiles map[string]bounty.Profile `json:"bountyProfiles,omitempty"`
	// Personas store operator-declared system-prompt additions. Like the bounty
	// scope, only the operator's own text is stored — never request bodies or
	// model responses.
	Personas map[string]persona.Persona `json:"personas,omitempty"`
	// ModelBindings pair a model name with a target model and/or a persona —
	// the mechanism behind names like "glm-5.3-mod". Distinct from the kv alias
	// table, which can only rename.
	ModelBindings map[string]modelalias.Binding `json:"modelBindings,omitempty"`
	// PersonasEnabled turns the persona plane on. It is off by default: a stored
	// persona does not affect any request until the operator enables it or names
	// it explicitly with the X-9Router-Persona header.
	PersonasEnabled bool `json:"personasEnabled,omitempty"`
	// DefaultPersona names the persona applied when the plane is enabled and the
	// request carries no X-9Router-Persona header.
	DefaultPersona string `json:"defaultPersona,omitempty"`
}

// DefaultSettings returns fallback settings.
func DefaultSettings() *SettingsData {
	return &SettingsData{
		RTKEnabled:        true,
		CavemanEnabled:    false,
		CavemanLevel:      "full",
		PonytailEnabled:   false,
		PonytailLevel:     "full",
		HeadroomUrl:       "http://localhost:8787",
		HeadroomKompress:  true,
		HeadroomTimeoutMs: 3000,
		AutoUpdate:        false,
		CapacityAdapter: map[string]CapacityAdapterEntry{
			"vision":     {Enabled: true, RoundRobin: false, Models: []string{"ag/gemini-3.8-flash-high"}},
			"audioInput": {Enabled: true, RoundRobin: false, Models: []string{}},
		},
		BountyProfiles: make(map[string]bounty.Profile),
		Personas:       make(map[string]persona.Persona),
		ModelBindings:  make(map[string]modelalias.Binding),
	}
}

// GetSettings reads settings row id = 1 from SQLite settings table.
func (r *Repo) GetSettings() (*SettingsData, error) {
	var rawData string
	err := r.db.QueryRow(`SELECT data FROM settings WHERE id = 1`).Scan(&rawData)
	if err != nil {
		return DefaultSettings(), nil
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(rawData), &raw); err != nil {
		return DefaultSettings(), nil
	}

	s := DefaultSettings()
	if v, ok := raw["rtkEnabled"].(bool); ok {
		s.RTKEnabled = v
	}
	if v, ok := raw["cavemanEnabled"].(bool); ok {
		s.CavemanEnabled = v
	}
	if lvl := handlerutil.GetString(raw, "cavemanLevel"); lvl != "" {
		s.CavemanLevel = lvl
	}
	if v, ok := raw["ponytailEnabled"].(bool); ok {
		s.PonytailEnabled = v
	}
	if lvl := handlerutil.GetString(raw, "ponytailLevel"); lvl != "" {
		s.PonytailLevel = lvl
	}
	if v := handlerutil.GetString(raw, "headroomUrl"); v != "" {
		s.HeadroomUrl = v
	}
	if v, ok := raw["headroomCodeAware"].(bool); ok {
		s.HeadroomCodeAware = v
	}
	if v, ok := raw["headroomKompress"].(bool); ok {
		s.HeadroomKompress = v
	}
	if v, ok := raw["headroomTimeoutMs"].(float64); ok && v > 0 {
		s.HeadroomTimeoutMs = int(v)
	}
	if v, ok := raw["autoUpdate"].(bool); ok {
		s.AutoUpdate = v
	}

	// Global provider fallback strategy
	if fs := handlerutil.GetString(raw, "fallbackStrategy"); fs != "" {
		s.FallbackStrategy = fs
	}
	if v, ok := raw["stickyRoundRobinLimit"].(float64); ok && v > 0 {
		s.StickyRoundRobinLimit = int(v)
	}

	// Global combo strategy
	if cs := handlerutil.GetString(raw, "comboStrategy"); cs != "" {
		s.ComboStrategy = cs
	}
	if v, ok := raw["comboStickyRoundRobinLimit"].(float64); ok && v > 0 {
		s.ComboStickyRoundRobinLimit = int(v)
	}

	// Per-combo strategies (Next.js & Svelte dashboards write `fallbackStrategy` / `strategy`, `stickyLimit` / `stickyRoundRobinLimit`, `judgeModel`)
	if csMap, ok := raw["comboStrategies"].(map[string]any); ok {
		s.ComboStrategies = make(map[string]ComboStrategy, len(csMap))
		for k, v := range csMap {
			if vm, ok := v.(map[string]any); ok {
				strat := handlerutil.GetString(vm, "strategy")
				if strat == "" {
					strat = handlerutil.GetString(vm, "fallbackStrategy")
				}
				sticky := 0
				if sl, ok := vm["stickyLimit"].(float64); ok && sl > 0 {
					sticky = int(sl)
				} else if sl, ok := vm["stickyRoundRobinLimit"].(float64); ok && sl > 0 {
					sticky = int(sl)
				}
				judge := handlerutil.GetString(vm, "judgeModel")
				s.ComboStrategies[k] = ComboStrategy{
					Strategy:    strat,
					StickyLimit: sticky,
					JudgeModel:  judge,
				}
			}
		}
	}

	// Per-provider strategies (Dashboards write `fallbackStrategy` / `rotateStrategy`, `stickyRoundRobinLimit` / `stickyLimit`)
	if ps, ok := raw["providerStrategies"].(map[string]any); ok {
		s.ProviderStrategies = make(map[string]ProviderStrategy, len(ps))
		for k, v := range ps {
			if vm, ok := v.(map[string]any); ok {
				rotateStrat := handlerutil.GetString(vm, "rotateStrategy")
				if rotateStrat == "" {
					rotateStrat = handlerutil.GetString(vm, "fallbackStrategy")
				}
				sticky := 0
				if sl, ok := vm["stickyLimit"].(float64); ok && sl > 0 {
					sticky = int(sl)
				} else if sl, ok := vm["stickyRoundRobinLimit"].(float64); ok && sl > 0 {
					sticky = int(sl)
				}
				strat := ProviderStrategy{
					ProxyPoolID:    handlerutil.GetString(vm, "proxyPoolId"),
					RotateStrategy: rotateStrat,
					StickyLimit:    sticky,
				}
				if sma, ok := vm["strictModelAssignment"].(bool); ok {
					strat.StrictModelAssignment = sma
				}
				s.ProviderStrategies[k] = strat
			}
		}
	}

	// Bounty profiles are decoded independently so a malformed profile does not
	// erase unrelated settings such as token savers or provider strategies.
	if profilesRaw, ok := raw["bountyProfiles"].(map[string]any); ok {
		profiles := make(map[string]bounty.Profile, len(profilesRaw))
		for id, value := range profilesRaw {
			// The map key is authoritative, so a stale or mismatched id inside the
			// record cannot make one request header select a different profile than
			// the request named. It must be applied BEFORE validation, otherwise a
			// record with an invalid nested id is skipped even though the key is
			// valid — silently dropping a profile the dashboard shows as saved.
			if !bounty.ValidProfileID(id) {
				continue
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				continue
			}
			var profile bounty.Profile
			if err := json.Unmarshal(encoded, &profile); err != nil {
				continue
			}
			profile.ID = id
			if profile.Validate() != nil {
				continue
			}
			profiles[id] = profile
		}
		s.BountyProfiles = profiles
	}

	// Personas are decoded independently for the same reason as bounty profiles:
	// one malformed record must not erase unrelated settings. The map key is
	// authoritative and is applied BEFORE validation, so a record with a stale
	// nested id is still usable under its real key rather than being silently
	// dropped even though the dashboard shows it as saved.
	if personasRaw, ok := raw["personas"].(map[string]any); ok {
		personas := make(map[string]persona.Persona, len(personasRaw))
		for id, value := range personasRaw {
			if !persona.ValidPersonaID(id) {
				continue
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				continue
			}
			var p persona.Persona
			if err := json.Unmarshal(encoded, &p); err != nil {
				continue
			}
			p.ID = id
			if p.Validate() != nil {
				continue
			}
			personas[id] = p
		}
		s.Personas = personas
	}
	if v, ok := raw["personasEnabled"].(bool); ok {
		s.PersonasEnabled = v
	}
	// The default is kept as written even when it names a persona that is not
	// stored: callers resolve it through GetPersona and fail closed, so the
	// operator sees a broken selector instead of silently unpersonified
	// requests.
	if v := handlerutil.GetString(raw, "defaultPersona"); v != "" {
		s.DefaultPersona = v
	}

	// Model aliases are decoded like personas: one malformed row must not erase
	// unrelated settings, and the map key is authoritative, so a record whose
	// nested id drifted is still usable under its real key.
	if bindingsRaw, ok := raw["modelBindings"].(map[string]any); ok {
		bindings := make(map[string]modelalias.Binding, len(bindingsRaw))
		for id, value := range bindingsRaw {
			if !modelalias.ValidID(id) {
				continue
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				continue
			}
			var b modelalias.Binding
			if err := json.Unmarshal(encoded, &b); err != nil {
				continue
			}
			b.ID = id
			if b.Validate() != nil {
				continue
			}
			bindings[id] = b
		}
		s.ModelBindings = bindings
	}

	// Capacity adapter pools (vision, audioInput, etc.)
	if caRaw, ok := raw["capacityAdapter"].(map[string]any); ok {
		s.CapacityAdapter = make(map[string]CapacityAdapterEntry, len(caRaw))
		for k, v := range caRaw {
			if vm, ok := v.(map[string]any); ok {
				enabled := true
				if en, ok := vm["enabled"].(bool); ok {
					enabled = en
				}
				rr := false
				if r, ok := vm["roundRobin"].(bool); ok {
					rr = r
				}
				var models []string
				if rawModels, ok := vm["models"].([]any); ok {
					for _, rm := range rawModels {
						if ms, ok := rm.(string); ok && ms != "" {
							if ms == "oc/mimo-v2.5-free" {
								ms = "oc/mimo-v2.6-flash-free"
							}
							models = append(models, ms)
						}
					}
				}
				s.CapacityAdapter[k] = CapacityAdapterEntry{
					Enabled:    enabled,
					RoundRobin: rr,
					Models:     models,
				}
			}
		}
	}

	return s, nil
}

// SetAutoUpdate updates the autoUpdate flag in the settings table.
func (r *Repo) SetAutoUpdate(enabled bool) error {
	return r.UpdateSettingsRaw(map[string]any{
		"autoUpdate": enabled,
	})
}

// SetProviderStrategy updates or sets the routing strategy and proxy pool for a provider without clobbering other settings.
func (r *Repo) SetProviderStrategy(provider string, strat ProviderStrategy) error {
	raw, err := r.GetSettingsRaw()
	if err != nil || raw == nil {
		raw = make(map[string]any)
	}

	var currentMap map[string]any
	if ps, ok := raw["providerStrategies"].(map[string]any); ok {
		currentMap = ps
	} else {
		currentMap = make(map[string]any)
	}

	entry := map[string]any{}
	if existingEntry, ok := currentMap[provider].(map[string]any); ok {
		for ek, ev := range existingEntry {
			entry[ek] = ev
		}
	}

	if strat.ProxyPoolID != "" {
		entry["proxyPoolId"] = strat.ProxyPoolID
	}
	if strat.RotateStrategy != "" {
		entry["rotateStrategy"] = strat.RotateStrategy
		entry["fallbackStrategy"] = strat.RotateStrategy
	}
	if strat.StickyLimit > 0 {
		entry["stickyLimit"] = strat.StickyLimit
		entry["stickyRoundRobinLimit"] = strat.StickyLimit
	}
	entry["strictModelAssignment"] = strat.StrictModelAssignment

	currentMap[provider] = entry
	return r.UpdateSettingsRaw(map[string]any{
		"providerStrategies": currentMap,
	})
}

// SetComboStrategy updates or sets the routing strategy for a combo without clobbering other settings.
func (r *Repo) SetComboStrategy(comboName string, strat ComboStrategy) error {
	raw, err := r.GetSettingsRaw()
	if err != nil || raw == nil {
		raw = make(map[string]any)
	}

	var currentMap map[string]any
	if cs, ok := raw["comboStrategies"].(map[string]any); ok {
		currentMap = cs
	} else {
		currentMap = make(map[string]any)
	}

	entry := map[string]any{}
	if existingEntry, ok := currentMap[comboName].(map[string]any); ok {
		for ek, ev := range existingEntry {
			entry[ek] = ev
		}
	}

	if strat.Strategy != "" {
		entry["strategy"] = strat.Strategy
		entry["fallbackStrategy"] = strat.Strategy
	}
	if strat.StickyLimit > 0 {
		entry["stickyLimit"] = strat.StickyLimit
		entry["stickyRoundRobinLimit"] = strat.StickyLimit
	}
	if strat.JudgeModel != "" {
		entry["judgeModel"] = strat.JudgeModel
	}

	if strat.Strategy == "fallback" && strat.JudgeModel == "" {
		delete(currentMap, comboName)
	} else {
		currentMap[comboName] = entry
	}

	return r.UpdateSettingsRaw(map[string]any{
		"comboStrategies": currentMap,
	})
}

// SetBountyProfile saves an authorization profile without clobbering unrelated
// settings. A profile is not a request log: only the operator-declared scope and
// rules are stored, never prompts or responses.
func (r *Repo) SetBountyProfile(profile bounty.Profile) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	raw, err := r.GetSettingsRaw()
	if err != nil || raw == nil {
		raw = make(map[string]any)
	}
	profiles, _ := raw["bountyProfiles"].(map[string]any)
	if profiles == nil {
		profiles = make(map[string]any)
	}
	profiles[profile.ID] = profile
	return r.UpdateSettingsRaw(map[string]any{"bountyProfiles": profiles})
}

// DeleteBountyProfile removes one scope profile. It does not retain the prompt
// text anywhere else.
func (r *Repo) DeleteBountyProfile(id string) error {
	raw, err := r.GetSettingsRaw()
	if err != nil || raw == nil {
		raw = make(map[string]any)
	}
	profiles, _ := raw["bountyProfiles"].(map[string]any)
	if profiles == nil {
		return nil
	}
	delete(profiles, id)
	return r.UpdateSettingsRaw(map[string]any{"bountyProfiles": profiles})
}

// GetBountyProfile returns a named profile, if configured.
func (r *Repo) GetBountyProfile(id string) (*bounty.Profile, error) {
	settings, err := r.GetSettings()
	if err != nil {
		return nil, err
	}
	profile, ok := settings.BountyProfiles[id]
	if !ok {
		return nil, nil
	}
	return &profile, nil
}

// SetPersona saves an operator-declared persona without clobbering unrelated
// settings. Only the operator's own prompt text is stored; request bodies and
// model responses are never written here.
func (r *Repo) SetPersona(p persona.Persona) error {
	if err := p.Validate(); err != nil {
		return err
	}
	raw, err := r.GetSettingsRaw()
	if err != nil || raw == nil {
		raw = make(map[string]any)
	}
	personas, _ := raw["personas"].(map[string]any)
	if personas == nil {
		personas = make(map[string]any)
	}
	personas[p.ID] = p
	return r.UpdateSettingsRaw(map[string]any{"personas": personas})
}

// DeletePersona removes one persona. It does not retain the prompt text
// anywhere else, and it leaves `defaultPersona` pointing at the deleted key on
// purpose: resolution then fails closed, so the operator notices the broken
// default instead of quietly losing the persona on every request.
func (r *Repo) DeletePersona(id string) error {
	raw, err := r.GetSettingsRaw()
	if err != nil || raw == nil {
		raw = make(map[string]any)
	}
	personas, _ := raw["personas"].(map[string]any)
	if personas == nil {
		return nil
	}
	delete(personas, id)
	return r.UpdateSettingsRaw(map[string]any{"personas": personas})
}

// GetPersona returns a named persona, if configured.
func (r *Repo) GetPersona(id string) (*persona.Persona, error) {
	settings, err := r.GetSettings()
	if err != nil {
		return nil, err
	}
	p, ok := settings.Personas[id]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

// SetPersonasPlane switches the persona plane on or off and sets its default
// persona in one read-modify-write, so the two cannot drift apart.
//
// A non-empty default must name a persona that is actually stored: accepting a
// dangling default would make every request fail closed later, which is a
// confusing way to learn about a typo. Clearing the default is always allowed.
func (r *Repo) SetPersonasPlane(enabled bool, defaultPersona string) error {
	defaultPersona = strings.TrimSpace(defaultPersona)
	if defaultPersona != "" {
		if !persona.ValidPersonaID(defaultPersona) {
			return fmt.Errorf("default persona id must be 1-%d letters, digits, '.', '_' or '-'", persona.MaxPersonaIDLength)
		}
		stored, err := r.GetPersona(defaultPersona)
		if err != nil {
			return err
		}
		if stored == nil {
			return fmt.Errorf("unknown persona %q; store it before making it the default", defaultPersona)
		}
	}
	return r.UpdateSettingsRaw(map[string]any{
		"personasEnabled": enabled,
		"defaultPersona":  defaultPersona,
	})
}

// PersonasBindingDefault reports whether id is the configured default persona.
// It lets delete handlers tell the operator their default still references the
// key they just removed.
func (r *Repo) PersonasBindingDefault(id string) (bool, error) {
	settings, err := r.GetSettings()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(settings.DefaultPersona) == id, nil
}

// SetModelBinding saves one binding without clobbering unrelated settings.
//
// The binding is validated here as well as in the handler so a programmatic
// caller cannot store a key no request can ever select.
func (r *Repo) SetModelBinding(b modelalias.Binding) error {
	if err := b.Validate(); err != nil {
		return err
	}
	raw, err := r.GetSettingsRaw()
	if err != nil || raw == nil {
		raw = make(map[string]any)
	}
	bindings, _ := raw["modelBindings"].(map[string]any)
	if bindings == nil {
		bindings = make(map[string]any)
	}
	bindings[b.ID] = b
	return r.UpdateSettingsRaw(map[string]any{"modelBindings": bindings})
}

// DeleteModelBinding removes one binding. Requests naming it afterwards are
// treated as naming an unknown model unless the kv alias table still renames it,
// which is an explicit outcome rather than a silent fallback.
func (r *Repo) DeleteModelBinding(id string) error {
	raw, err := r.GetSettingsRaw()
	if err != nil || raw == nil {
		return nil
	}
	bindings, _ := raw["modelBindings"].(map[string]any)
	if bindings == nil {
		return nil
	}
	delete(bindings, id)
	return r.UpdateSettingsRaw(map[string]any{"modelBindings": bindings})
}

// GetModelBinding returns one binding, if configured. The kv alias table has its
// own GetModelAlias for plain renames; this one carries the persona as well.
func (r *Repo) GetModelBinding(id string) (*modelalias.Binding, error) {
	settings, err := r.GetSettings()
	if err != nil {
		return nil, err
	}
	b, ok := settings.ModelBindings[id]
	if !ok {
		return nil, nil
	}
	return &b, nil
}

// ModelBindingsReferencingPersona lists binding ids bound to a persona. The
// persona delete handler uses it to warn that removing a persona breaks those
// bindings.
func (r *Repo) ModelBindingsReferencingPersona(personaID string) ([]string, error) {
	settings, err := r.GetSettings()
	if err != nil {
		return nil, err
	}
	var ids []string
	for id, b := range settings.ModelBindings {
		if b.Persona == personaID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// PersonaIDs returns every stored persona id, sorted. It exists so callers can
// render a stable list without reaching into the settings map and re-sorting.
func (r *Repo) PersonaIDs() ([]string, error) {
	settings, err := r.GetSettings()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(settings.Personas))
	for id := range settings.Personas {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
