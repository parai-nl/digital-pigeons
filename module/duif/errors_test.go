package duif

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Capfout-contract: de LiteLLM-budgetfout wordt op
// een stabiele reason-code gemapt; alle andere LLM-fouten blijven generiek.
func TestClassifyLLMError_Budget(t *testing.T) {
	// Nagebootste upstream-fouttekst zoals de proxy die geeft (testdata).
	upstream := fmt.Errorf("litellm stream: %w",
		errors.New("BudgetExceededError: Budget has been exceeded! Current cost: 9.87654"))
	if got := classifyLLMError(upstream); got != ReasonBudgetExceeded {
		t.Errorf("classifyLLMError = %q, want %q", got, ReasonBudgetExceeded)
	}
	if got := classifyLLMError(fmt.Errorf("wrap: %w", ErrBudgetExceeded)); got != ReasonBudgetExceeded {
		t.Errorf("sentinel ErrBudgetExceeded moet op %q mappen, got %q", ReasonBudgetExceeded, got)
	}
}

func TestClassifyLLMError_OtherStaysGeneric(t *testing.T) {
	for _, err := range []error{
		errors.New("litellm complete: connection refused"),
		errors.New("context deadline exceeded"),
	} {
		if got := classifyLLMError(err); got != ReasonLLMUnavailable {
			t.Errorf("classifyLLMError(%v) = %q, want %q", err, got, ReasonLLMUnavailable)
		}
	}
}

// Geen fout is geen reason-code: de caller mag hier nooit een lege
// error-frame op bouwen.
func TestClassifyLLMError_NilIsEmpty(t *testing.T) {
	if got := classifyLLMError(nil); got != "" {
		t.Errorf("classifyLLMError(nil) = %q, want leeg", got)
	}
}

// Een onbekende code valt terug op llm_unavailable, zodat er nooit een lege
// melding op de wire staat.
func TestErrorBody_UnknownReason_FallsBackToUnavailableMessage(t *testing.T) {
	body := errorBody("iets_totaal_nieuws")
	if body.Error != "iets_totaal_nieuws" {
		t.Errorf("body.Error = %q, want de doorgegeven code", body.Error)
	}
	want := errorMessages[ReasonLLMUnavailable]
	if body.Message["en"] != want["en"] || body.Message["nl"] != want["nl"] {
		t.Errorf("message = %+v, want de llm_unavailable-melding", body.Message)
	}
}

// De foutbody volgt het error/message-contract met beide talen en bevat nooit
// de upstream-fouttekst (PII/lek-regel). De lus loopt over de catalogus zelf en
// niet over een handgeschreven rijtje: dat rijtje noemde vier van de acht
// redenen, zodat ReasonInvalidRequest en de graafredenen ongetoetst bleven en
// een nieuwe reden er stilletjes buiten viel.
func TestErrorBody_Contract(t *testing.T) {
	if len(errorMessages) == 0 {
		t.Fatal("de foutcatalogus is leeg")
	}
	for reason, catalogue := range errorMessages {
		body := errorBody(reason)
		if body.Error != reason {
			t.Errorf("body.Error = %q, want %q", body.Error, reason)
		}
		if body.Message["en"] != catalogue["en"] || body.Message["nl"] != catalogue["nl"] {
			t.Errorf("%s: body draagt een andere zin dan de catalogus: %+v", reason, body.Message)
		}
		if body.Message["en"] == "" || body.Message["nl"] == "" {
			t.Errorf("%s: message moet en én nl dragen: %+v", reason, body.Message)
		}
		// Identiek nl en en is de fout die dit contract wegnam: de Engelse zin
		// die ongewijzigd in het Nederlandse slot belandt.
		if body.Message["en"] == body.Message["nl"] {
			t.Errorf("%s: nl is niet vertaald, het is de Engelse zin: %q", reason, body.Message["nl"])
		}
		if len(body.Message) != 2 {
			t.Errorf("%s: message draagt onbekende talen: %+v", reason, body.Message)
		}
	}
}

// Elke 400-reden staat in de catalogus, en de catalogus kent geen reden buiten
// de lijst. De toets hieronder loopt over de map, dus een nieuwe constante
// zonder catalogusregel viel daar stilzwijgend buiten: hij belandde ongemerkt
// op de algemene zin en de gebruiker las niet wat hij kon doen.
func TestBadRequestMessages_EveryReasonIsInTheCatalogue(t *testing.T) {
	inLijst := map[badRequestReason]bool{}
	for _, reason := range allBadRequestReasons {
		if inLijst[reason] {
			t.Errorf("%s staat twee keer in allBadRequestReasons", reason)
		}
		inLijst[reason] = true
		if _, ok := badRequestMessages[reason]; !ok {
			t.Errorf("%s heeft geen catalogusregel: de gebruiker krijgt de algemene zin", reason)
		}
	}
	for reason := range badRequestMessages {
		if !inLijst[reason] {
			t.Errorf("%s staat in de catalogus maar niet in allBadRequestReasons", reason)
		}
	}
	// En de weg erheen klopt ook werkelijk: elke reden levert haar eigen zin,
	// niet de algemene.
	algemeen := badRequestMessages[badRequestReason("bestaat-niet")]
	for _, reason := range allBadRequestReasons {
		body := badRequest(reason).body
		if body.Message["nl"] == "" || (algemeen != nil && body.Message["nl"] == algemeen["nl"]) {
			t.Errorf("%s valt terug op de algemene zin: %+v", reason, body.Message)
		}
	}
}

// Elke 400-reden draagt twee verschillende talen. Een identieke en/nl is het
// symptoom van de fout die dit contract moest wegnemen: de Engelse
// ontwikkelaarszin die ongewijzigd in het Nederlandse slot belandde.
func TestBadRequestMessages_BilingualAndDistinct(t *testing.T) {
	for reason, msg := range badRequestMessages {
		if msg["en"] == "" || msg["nl"] == "" {
			t.Errorf("%s: melding moet en én nl dragen: %+v", reason, msg)
		}
		if msg["en"] == msg["nl"] {
			t.Errorf("%s: nl is niet vertaald, het is de Engelse zin: %q", reason, msg["nl"])
		}
		if len(msg) != 2 {
			t.Errorf("%s: melding draagt onbekende talen: %+v", reason, msg)
		}
	}
}

// De wire-code blijft invalid_request — de client kent hem en heeft er een
// eigen sleutel voor — en de body draagt de zin die bij de reden hoort.
func TestBadRequest_UsesInvalidRequestCodeWithItsOwnMessage(t *testing.T) {
	te := badRequest(reasonUnsupportedLang)
	if te.status != 400 {
		t.Errorf("status = %d, want 400", te.status)
	}
	if te.body.Error != ReasonInvalidRequest {
		t.Errorf("body.Error = %q, want %q", te.body.Error, ReasonInvalidRequest)
	}
	want := badRequestMessages[reasonUnsupportedLang]
	if te.body.Message["nl"] != want["nl"] || te.body.Message["en"] != want["en"] {
		t.Errorf("message = %+v, want %+v", te.body.Message, want)
	}
}

// Een reden zonder catalogusregel mag geen leeg message-slot opleveren: de
// client toont de servermelding en zou dan niets tonen.
func TestBadRequest_UnknownReason_FallsBackToGeneralMessage(t *testing.T) {
	body := badRequest(badRequestReason("nog_niet_bestaande_reden")).body
	want := errorMessages[ReasonInvalidRequest]
	if body.Message["en"] != want["en"] || body.Message["nl"] != want["nl"] {
		t.Errorf("message = %+v, want de algemene invalid_request-melding", body.Message)
	}
}

// badRequestReasonSource leest de broncode van het pakket van schijf en geeft
// twee naamlijsten terug: de constanten van type badRequestReason, en de namen
// die in de composite literal van allBadRequestReasons staan.
//
// Van schijf en niet via reflectie: een constante die nergens genoemd wordt
// bestaat op runtime niet, dus alleen de broncode weet dat hij er is. Dat is
// precies het gat dat hier dicht moet — een nieuwe reden die noch in de lijst
// noch in de catalogus belandt kwam er anders stil doorheen.
//
// Alle niet-testbestanden gaan mee, want een volgende reden hoeft niet per se
// in errors.go te worden opgeschreven; errors.go moet er wél constanten in
// hebben, anders leest deze toets een pakket dat hij niet herkent.
func badRequestReasonSource(t *testing.T) (constanten, inLijst []string) {
	t.Helper()
	paden, err := filepath.Glob("*.go")
	if err != nil || len(paden) == 0 {
		t.Fatalf("pakketbestanden niet te vinden: %v", err)
	}
	fset := token.NewFileSet()
	lijstGevonden := false
	uitErrorsGo := 0
	for _, pad := range paden {
		if strings.HasSuffix(pad, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, pad, nil, 0)
		if err != nil {
			t.Fatalf("%s niet te parsen: %v", pad, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			switch gd.Tok {
			case token.CONST:
				// Binnen één const-blok erft een regel zonder type en zonder
				// waarde het type van de regel erboven; zonder die regel zou
				// een iota-achtige voortzetting stil buiten de toets vallen.
				huidig := ""
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					if id, ok := vs.Type.(*ast.Ident); ok {
						huidig = id.Name
					} else if vs.Type != nil || len(vs.Values) > 0 {
						huidig = ""
					}
					if huidig != "badRequestReason" {
						continue
					}
					for _, naam := range vs.Names {
						constanten = append(constanten, naam.Name)
						if pad == "errors.go" {
							uitErrorsGo++
						}
					}
				}
			case token.VAR:
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "allBadRequestReasons" {
						continue
					}
					if len(vs.Values) != 1 {
						t.Fatalf("allBadRequestReasons heeft geen enkele waarde in %s", pad)
					}
					lit, ok := vs.Values[0].(*ast.CompositeLit)
					if !ok {
						t.Fatalf("allBadRequestReasons is geen literal-lijst in %s", pad)
					}
					lijstGevonden = true
					for _, el := range lit.Elts {
						id, ok := el.(*ast.Ident)
						if !ok {
							t.Fatalf("allBadRequestReasons draagt een regel die geen constante noemt in %s", pad)
						}
						inLijst = append(inLijst, id.Name)
					}
				}
			}
		}
	}
	if uitErrorsGo == 0 {
		t.Fatal("geen enkele badRequestReason-constante gevonden in errors.go")
	}
	if !lijstGevonden {
		t.Fatal("allBadRequestReasons niet gevonden in de broncode")
	}
	return constanten, inLijst
}

// De ketting moet heel zijn: elke constante van type badRequestReason staat in
// allBadRequestReasons, en TestBadRequestMessages_EveryReasonIsInTheCatalogue
// haakt die lijst aan de tweetalige catalogus. Zonder deze schakel viel een
// nieuwe constante die niemand in de lijst zette stil terug op de algemene
// zin.
func TestBadRequestReasons_EveryConstantIsInTheList(t *testing.T) {
	constanten, inLijst := badRequestReasonSource(t)
	lijst := map[string]bool{}
	for _, naam := range inLijst {
		if lijst[naam] {
			t.Errorf("%s staat twee keer in allBadRequestReasons", naam)
		}
		lijst[naam] = true
	}
	bestaat := map[string]bool{}
	for _, naam := range constanten {
		bestaat[naam] = true
		if !lijst[naam] {
			t.Errorf("de constante %s staat niet in allBadRequestReasons: een 400 met deze reden leest als de algemene zin", naam)
		}
	}
	for _, naam := range inLijst {
		if !bestaat[naam] {
			t.Errorf("allBadRequestReasons noemt %s, maar dat is geen badRequestReason-constante", naam)
		}
	}
	// En de gelezen lijst is werkelijk de lijst die de code gebruikt.
	if len(inLijst) != len(allBadRequestReasons) {
		t.Errorf("de broncode telt %d regels in allBadRequestReasons, de code %d", len(inLijst), len(allBadRequestReasons))
	}
}
