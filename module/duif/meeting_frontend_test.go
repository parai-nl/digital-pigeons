package duif

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// De module-frontend (Svelte 5 + @parai/ui) wordt gebouwd naar
// frontend/dist/bundle.js; de bron staat in frontend/src. Deze Go-tests
// bewaken vanuit Go wat vanuit Go te bewaken is: dat de bron geen enkele
// opslag-, opname-, download- of kanaal-API aanraakt (harde eis: vergaderinhoud
// blijft vluchtig), dat de TypeScript-protocoltabel elk serverwoord kent, en —
// als er een bundle gebouwd is — dat die schoon en binnen budget is. Het gedrag
// zelf staat in de vitest-suite (make check).

const frontendSrc = "frontend/src"

// frontendSourceFiles levert alle bronbestanden van de module-frontend,
// zonder tests en testhulpen (die mogen fakes van opslag-API's bevatten).
func frontendSourceFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(frontendSrc, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testing" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".ts", ".js", ".svelte", ".css", ".html":
		default:
			return nil
		}
		if strings.HasSuffix(path, ".test.ts") || strings.HasSuffix(path, ".d.ts") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(path)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("frontend-bron lezen: %v", err)
	}
	// Een hernoemde map mag de toets niet stilletjes uitschakelen.
	if len(files) < 10 {
		t.Fatalf("slechts %d bronbestanden gevonden onder %s; is de frontend verplaatst?", len(files), frontendSrc)
	}
	return files
}

// Overal verboden: opslag, opname, bestanden, downloads, persistentie via
// history/window.name, kanalen naar buiten, scripting die tekst als HTML
// injecteert.
var frontendForbiddenEverywhere = regexp.MustCompile(strings.Join([]string{
	`localStorage`, `sessionStorage`, `indexedDB`, `openDatabase`,
	`MediaRecorder`, `document\.cookie`, `cookieStore`,
	`showSaveFilePicker`, `showOpenFilePicker`, `showDirectoryPicker`,
	`createObjectURL`, `FileSystemWritableFileStream`, `requestFileSystem`,
	`navigator\.storage`, `caches\.`, `BroadcastChannel`, `sendBeacon`,
	`serviceWorker`, `captureStream`, `MediaStreamTrackProcessor`,
	`pushState`, `replaceState`, `window\.name\b`, `postMessage`, `\bopener\b`,
	`RTCPeerConnection`, `WebSocket`, `WebTransport`, `SpeechRecognition`,
	`getDisplayMedia`, `clipboard`, `execCommand`, `\bdownload\b`,
	`\{@html`, `innerHTML`, `outerHTML`, `insertAdjacentHTML`,
	// Verzenden buiten de fetch-laag om: XHR ontsnapt aan elke afspraak
	// die de portlaag maakt, navigator.share geeft de inhoud aan een
	// wíllekeurige app, en een nieuw venster of een navigatie neemt de
	// sessie mee naar een adres dat deze module niet kent.
	`XMLHttpRequest`, `navigator\.share`, `window\.open\b`,
	`location\.assign`, `location\.replace`,
	// Een worker is een tweede uitvoeringscontext: wat daarheen gaat valt
	// buiten deze scan, dus valt hij buiten de module.
	`\bWorker\b`, `\bSharedWorker\b`,
	// Een canvas is sinds de kennisgraaf een beeld van kennisbankinhoud.
	// Deze drie zijn de weg terug: van tekening naar bestand, byte of
	// pixel — dus naar iets wat te bewaren of te versturen is.
	`toDataURL`, `toBlob`, `getImageData`,
	// De omwegen eromheen. Een navigatie of adreswijziging door toewijzing
	// schrijft net zo goed in de geschiedenis als pushState;
	// een berekende sleutel op een globaal object ('session' + 'Storage',
	// history['push' + 'State']) ontsnapt aan elk letterlijk patroon
	// hierboven, en een losse 'Storage' in een string is daar de helft van.
	`location\.(?:hash|href|search|pathname)\s*=[^=]`,
	`(?:window|globalThis|self|document)\.location\s*=[^=]`,
	`(?:globalThis|window|self|document|navigator|history|location)\s*\[`,
	"['\"`]Storage['\"`]",
}, "|"))

// TestMeetingFrontend_TouchesNoStorageAPI bewaakt de eerste borging van de
// vluchtigheid: in de browser gaat vergaderaudio uitsluitend als stroom de deur
// uit en leeft het transcript alleen in het geheugen van de pagina. De regel
// geldt voor de hele module-bron. Alleen de gevalideerde EN/NL-voorkeuradapter,
// het vaste ontvangende authkanaal en de plek van de lezer in het adresfragment
// zijn uitgezonderd; geen store krijgt opslagtoegang.
func TestMeetingFrontend_TouchesNoStorageAPI(t *testing.T) {
	files := frontendSourceFiles(t)

	everywhere := frontendForbiddenEverywhere
	// Onder src/lib (de gebundelde modulecode): geen logging van state of
	// inhoud, geen netwerkadressen buiten de eigen API.
	inLib := regexp.MustCompile(`\$inspect|console\.|https?://`)
	// De vergader- en audiolagen praten nooit met shell-kanalen: vergaderinhoud
	// verlaat de module niet via toasts of de event-bus.
	// Binary texture decoding is ordinary rendering. Keep recording/file
	// boundaries on the layers that actually handle transient meeting content.
	inMeeting := regexp.MustCompile(`ctx\.toast|ctx\.events|FileReader|new\s+Blob\b`)

	for path, src := range files {
		// The standalone design atelier has no meeting contents and is not a
		// production entry. The shared 3D renderer lives in lib/media and is
		// still inspected here; every shipped JS chunk is inspected separately.
		if strings.HasPrefix(path, frontendSrc+"/design/") {
			continue
		}
		if path == frontendSrc+"/lib/access/authLifecycle.ts" {
			// Only receiving the existing shell's fixed logout/expiry signal is
			// permitted. Sending (postMessage) stays forbidden even in this file.
			for _, required := range []string{"data.type !== 'logout' && data.type !== 'session_expired'", "channel.onmessage = null", "channel.close()", "end()"} {
				if !strings.Contains(src, required) {
					t.Errorf("auth lifecycle lacks its receive/cleanup boundary: %s", required)
				}
			}
			for _, allowed := range []string{"globalThis.BroadcastChannel", "new BroadcastChannel('parai-auth')"} {
				if strings.Count(src, allowed) != 1 {
					t.Errorf("auth lifecycle channel operation changed: %s", allowed)
				}
				src = strings.ReplaceAll(src, allowed, "fixedInboundAuthChannel")
			}
		}
		if path == frontendSrc+"/lib/shell/languagePreference.ts" {
			// Allow exactly these two fixed-key operations, not the file as a
			// whole. Every other storage/recording/exfiltration rule still applies.
			for _, required := range []string{"const LANGUAGE_KEY = 'duif.ui-language'", "if (!isLang(lang)) return", "isLang(saved)"} {
				if !strings.Contains(src, required) {
					t.Errorf("language preference lacks runtime boundary: %s", required)
				}
			}
			for _, allowed := range []string{"globalThis.localStorage?.getItem(LANGUAGE_KEY)", "globalThis.localStorage?.setItem(LANGUAGE_KEY, lang)"} {
				if strings.Count(src, allowed) != 1 {
					t.Errorf("language preference storage call changed: %s", allowed)
				}
				src = strings.ReplaceAll(src, allowed, "validatedLanguagePreference")
			}
		}
		if path == frontendSrc+"/lib/shell/workspacePlace.ts" {
			// De plek van de lezer (weergave, duif, knoop- en
			// gespreks-id, gevalideerd) in het fragment van het adres, zodat de
			// herlaad van een taalwissel haar niet wist. Precies één vervanging
			// van de huidige stap, met een url die alleen uit het eigen pad en
			// het gevalideerde fragment bestaat; pushState en elke andere
			// history-, opslag- of kanaal-API blijven ook hier verboden.
			for _, required := range []string{
				"const hash = workspaceHash(location.hash, place)",
				"const url = location.pathname + location.search + hash",
				"} = normalize(place)",
				"const NODE_ID = /^[a-z_]+:[0-9a-f-]{36}$/",
				"const SESSION_ID = /^[0-9a-f]{32}$/",
			} {
				if !strings.Contains(src, required) {
					t.Errorf("workspace place lacks its boundary: %s", required)
				}
			}
			const allowed = "globalThis.history.replaceState(globalThis.history.state, '', url)"
			if strings.Count(src, allowed) != 1 {
				t.Errorf("workspace place history call changed: %s", allowed)
			}
			src = strings.ReplaceAll(src, allowed, "validatedWorkspacePlace")
		}
		if m := everywhere.FindString(src); m != "" {
			t.Errorf("%s gebruikt %q; vergaderaudio en -transcript zijn streaming-only en worden nergens bewaard", path, m)
		}
		if strings.HasPrefix(path, frontendSrc+"/lib/") {
			if m := inLib.FindString(src); m != "" {
				t.Errorf("%s bevat %q; modulecode logt niets en kent geen externe adressen", path, m)
			}
		}
		if strings.HasPrefix(path, frontendSrc+"/lib/meeting/") || strings.HasPrefix(path, frontendSrc+"/lib/audio/") {
			if m := inMeeting.FindString(src); m != "" {
				t.Errorf("%s gebruikt %q; vergaderinhoud gaat nooit via shell-kanalen", path, m)
			}
		}
	}
}

// TestMeetingFrontend_GuardCatchesIndirectWrites houdt de omwegen rond de
// verboden API's vast: een toewijzing aan het adres en een berekende
// sleutel op een globaal object zijn even verboden als de API zelf, terwijl
// het lezen van het adres (workspacePlace.ts) gewoon mag.
func TestMeetingFrontend_GuardCatchesIndirectWrites(t *testing.T) {
	for _, bad := range []string{
		"location.hash = '#transcript=' + text",
		"globalThis.location.href='/elders'",
		"window.location = target",
		"location.search = params",
		"globalThis['session' + 'Storage'].setItem(k, v)",
		"window [`local${'Storage'}`]",
		"history['push' + 'State'](null, '', url)",
		"const key = 'Storage'",
	} {
		if !frontendForbiddenEverywhere.MatchString(bad) {
			t.Errorf("wachter laat door: %s", bad)
		}
	}
	for _, fine := range []string{
		"if (hash === location.hash || (hash === '' && !location.href.includes('#'))) return",
		"const url = location.pathname + location.search + hash",
		"const location = globalThis.location",
		"resumeWorkspace(services, { hash: window.location.hash, search: window.location.search })",
	} {
		if m := frontendForbiddenEverywhere.FindString(fine); m != "" {
			t.Errorf("wachter weigert gewoon lezen: %s (%q)", fine, m)
		}
	}
}

// TestMeetingFrontend_SpeaksTheServerProtocol koppelt de TypeScript-
// protocoltabel aan de Go-constanten: elk event dat de server kan sturen, elke
// luister- en stemstand, elke sluitreden en elke route staat er letterlijk in.
func TestMeetingFrontend_SpeaksTheServerProtocol(t *testing.T) {
	raw, err := os.ReadFile(frontendSrc + "/lib/meeting/protocol.ts")
	if err != nil {
		t.Fatalf("protocol.ts lezen: %v", err)
	}
	src := string(raw)

	words := []string{
		EventState, EventTranscript, EventPersona, EventText, EventSources,
		EventTextDone, EventTTSAudio,
		EventTTSDone, EventResponseDone, EventBusy, EventStopped, EventError,
		EventPresenceCheck, EventSessionClosed, EventListening, EventCandidate,
		EventArousal, EventIntervention, EventDetectorStatus, EventSound, EventSettings,
		string(ListenActive), string(ListenReconnecting), string(ListenOff),
		string(VoiceReady), string(VoiceOff),
		CloseReasonInactivity, CloseReasonUser, CloseReasonMaxDuration, CloseReasonShutdown,
		CloseReasonReplaced, CloseReasonClientGone,
	}
	for _, w := range words {
		if !strings.Contains(src, `'`+w+`'`) && !strings.Contains(src, `"`+w+`"`) {
			t.Errorf("protocol.ts kent %q niet", w)
		}
	}
	for _, route := range []string{
		"/meeting/sessions", "/events", "/audio", "/transcript", "/respond",
		"/intervene", "/settings", "/stop", "/presence", "/candidate/speak",
	} {
		if !strings.Contains(src, `'`+route+`'`) {
			t.Errorf("protocol.ts gebruikt de route %q niet", route)
		}
	}
}
