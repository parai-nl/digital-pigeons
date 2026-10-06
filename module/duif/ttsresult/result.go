// Package ttsresult bridges the public optional terminal-result field across
// the pinned core API and older local workspaces. Missing support is unknown,
// never successful completion. No private provider implementation is copied.
package ttsresult

import (
	"github.com/parai-nl/parai-core/audio"
	"reflect"
)

func Read(s *audio.SynthesizeSession) <-chan error {
	if s == nil {
		return nil
	}
	f := reflect.ValueOf(s).Elem().FieldByName("Result")
	if !f.IsValid() || !f.CanInterface() {
		return nil
	}
	ch, _ := f.Interface().(<-chan error)
	return ch
}

func Set(s *audio.SynthesizeSession, ch <-chan error) bool {
	if s == nil {
		return false
	}
	f := reflect.ValueOf(s).Elem().FieldByName("Result")
	value := reflect.ValueOf(ch)
	if !f.IsValid() || !f.CanSet() || !value.Type().AssignableTo(f.Type()) {
		return false
	}
	f.Set(value)
	return true
}
