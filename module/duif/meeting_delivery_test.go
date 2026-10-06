package duif

import (
	"strings"
	"testing"
)

// De beurt onthoudt wat er geleverd is. Een delta die emitTurn
// weigert is niet geleverd: hij bereikte geen scherm en geen stem. send boekte
// hem tóch al in de tekst van de beurt, en die tekst gaat als bijdrage het
// geheugen van de sessie in (noteContribution) — zo onthield de vergadering een
// zin die niemand gezien heeft.
func TestTurnDelivery_ARefusedDeltaIsNotPartOfTheDeliveredText(t *testing.T) {
	rig, _ := groundedMeetingRig(t, nil)
	s := rig.session(t, rig.openSession(t, "biological", "en"))
	gen, ctx, cancel, ok := s.beginTurn()
	if !ok {
		t.Fatal("het spreekslot was niet vrij")
	}
	defer cancel()
	sp := rig.engine.newSpeaker(ctx, s, gen)
	defer sp.close()

	d := &turnDelivery{s: s, gen: gen, sp: sp}
	const geleverd = "This sentence reached the table. "
	const geweigerd = "Nobody ever saw this one. "
	if !d.send(geleverd) {
		t.Fatal("de eerste delta werd al geweigerd")
	}

	// Een nieuwe beurt verhoogt de generatie: vanaf hier weigert emitTurn elke
	// chunk van de oude beurt.
	s.endTurn(gen)
	nieuw, _, cancelNieuw, ok := s.beginTurn()
	if !ok {
		t.Fatal("het spreekslot bleef bezet")
	}
	defer cancelNieuw()
	defer s.endTurn(nieuw)
	if d.send(geweigerd) {
		t.Fatal("emitTurn nam een chunk van een verlopen beurt alsnog aan")
	}

	if !strings.Contains(d.text(), geleverd) {
		t.Errorf("de geleverde zin ontbreekt in de tekst: %q", d.text())
	}
	if strings.Contains(d.text(), geweigerd) {
		t.Errorf("een geweigerde delta staat in de tekst die het label beschrijft: %q", d.text())
	}
	if d.length() != len(geleverd) {
		t.Errorf("tekens = %d, wil %d", d.length(), len(geleverd))
	}
}

// Een gesloten sessie weigert net zo goed, en ook daar blijft de tekst staan op
// wat er werkelijk geleverd is.
func TestTurnDelivery_AClosedSessionRefusesAndDoesNotBookTheDelta(t *testing.T) {
	rig, _ := groundedMeetingRig(t, nil)
	s := rig.session(t, rig.openSession(t, "biological", "en"))
	gen, ctx, cancel, ok := s.beginTurn()
	if !ok {
		t.Fatal("het spreekslot was niet vrij")
	}
	defer cancel()
	sp := rig.engine.newSpeaker(ctx, s, gen)
	defer sp.close()

	d := &turnDelivery{s: s, gen: gen, sp: sp}
	const geleverd = "This sentence reached the table. "
	if !d.send(geleverd) {
		t.Fatal("de eerste delta werd al geweigerd")
	}
	s.close("test")
	if d.send("Nobody ever saw this one. ") {
		t.Fatal("een gesloten sessie nam de chunk alsnog aan")
	}
	if d.text() != geleverd {
		t.Errorf("geleverde tekst = %q, wil %q", d.text(), geleverd)
	}
}
