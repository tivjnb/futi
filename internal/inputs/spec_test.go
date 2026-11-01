package inputs

import (
	"encoding/gob"
	"testing"
)

// TestSpecUsesPlainGob: Spec is encoded field by field. A custom GobEncoder
// would create a gob codec per Spec, which makes decoding the module cache
// (read on every call and every TAB) several times slower.
func TestSpecUsesPlainGob(t *testing.T) {
	if _, ok := any(Spec{}).(gob.GobEncoder); ok {
		t.Error("Spec implements gob.GobEncoder, want plain field encoding")
	}
	if _, ok := any(&Spec{}).(gob.GobDecoder); ok {
		t.Error("*Spec implements gob.GobDecoder, want plain field encoding")
	}
}
