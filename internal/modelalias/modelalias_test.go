package modelalias

import (
	"strings"
	"testing"
)

func TestBindingValidateRequiresAnEffect(t *testing.T) {
	cases := []struct {
		name    string
		binding Binding
		wantErr string
	}{
		{"rename only", Binding{ID: "glm-mod", Target: "glm-5.3", Enabled: true}, ""},
		{"persona only", Binding{ID: "glm-mod", Persona: "ltx-quasar", Enabled: true}, ""},
		{"both", Binding{ID: "glm-mod", Target: "glm-5.3", Persona: "ltx-quasar", Enabled: true}, ""},
		{"no effect", Binding{ID: "glm-mod", Enabled: true}, "does nothing"},
		{"self target", Binding{ID: "glm-mod", Target: "glm-mod"}, "cannot target itself"},
		{"bad id", Binding{ID: " bad", Target: "glm-5.3"}, "invalid model binding id"},
		{"bad persona", Binding{ID: "glm-mod", Persona: "bad persona!"}, "invalid persona id"},
	}
	for _, tc := range cases {
		err := tc.binding.Validate()
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v", tc.name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: expected an error mentioning %q", tc.name, tc.wantErr)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.wantErr)
		}
	}
}

// A disabled binding is the operator's off switch: it must remove both the
// rename and the persona without deleting the record.
func TestDisabledBindingResolvesNothing(t *testing.T) {
	b := Binding{ID: "glm-mod", Target: "glm-5.3", Persona: "ltx-quasar", Enabled: false}
	if target, ok := b.ResolveTarget(); ok || target != "" {
		t.Errorf("disabled binding rewrote the model to %q", target)
	}
	if id, ok := b.PersonaID(); ok || id != "" {
		t.Errorf("disabled binding selected persona %q", id)
	}
}

// A persona-only binding must not rewrite the model name: the point is to keep
// the catalog model while attaching prompt context.
func TestPersonaOnlyBindingKeepsTheModel(t *testing.T) {
	b := Binding{ID: "glm-mod", Persona: "ltx-quasar", Enabled: true}
	if target, ok := b.ResolveTarget(); ok {
		t.Errorf("persona-only binding rewrote the model to %q", target)
	}
	if id, ok := b.PersonaID(); !ok || id != "ltx-quasar" {
		t.Errorf("persona-only binding selected persona %q (ok=%v)", id, ok)
	}
}

// A rename-only binding must not attach prompt context.
func TestRenameOnlyBindingCarriesNoPersona(t *testing.T) {
	b := Binding{ID: "glm-mod", Target: "glm-5.3", Enabled: true}
	if id, ok := b.PersonaID(); ok {
		t.Errorf("rename-only binding selected persona %q", id)
	}
	if target, ok := b.ResolveTarget(); !ok || target != "glm-5.3" {
		t.Errorf("rename-only binding resolved to %q (ok=%v)", target, ok)
	}
}

func TestValidIDAcceptsGroupedNames(t *testing.T) {
	for _, id := range []string{"glm-mod", "team/glm-mod", "a.b_c-d", "9router-mod"} {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"", "-leading", "has space", strings.Repeat("x", MaxIDLength+1)} {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true, want false", id)
		}
	}
}
