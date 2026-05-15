package ir

import "testing"

// Compile-time assertions that block types satisfy the Block interface.
var (
	_ Block = ProseBlock{}
	_ Block = MachineBlock{}
	_ Block = KBRefBlock{}
	_ Block = TableBlock{}
	_ Block = DiagramBlock{}
	_ Block = Callout{}
	_ Block = EscapeHatch{}
)

func TestBlockZone_DefaultsAreCorrect(t *testing.T) {
	cases := []struct {
		name  string
		block Block
		want  Zone
	}{
		{"prose is editorial", ProseBlock{}, ZoneEditorial},
		{"machine is machine", MachineBlock{}, ZoneMachine},
		{"kbref is machine", KBRefBlock{}, ZoneMachine},
		{"table is machine", TableBlock{}, ZoneMachine},
		{"diagram is machine", DiagramBlock{}, ZoneMachine},
		{"callout-machine is machine", Callout{IsMachine: true}, ZoneMachine},
		{"callout-editorial is editorial", Callout{IsMachine: false}, ZoneEditorial},
		{"escape-hatch is machine", EscapeHatch{}, ZoneMachine},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.block.Zone(); got != tc.want {
				t.Errorf("Zone() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBlockKind_Distinct(t *testing.T) {
	kinds := map[string]bool{}
	for _, b := range []Block{
		ProseBlock{}, MachineBlock{}, KBRefBlock{},
		TableBlock{}, DiagramBlock{}, Callout{}, EscapeHatch{},
	} {
		k := b.Kind()
		if kinds[k] {
			t.Errorf("duplicate kind: %s", k)
		}
		kinds[k] = true
	}
}
