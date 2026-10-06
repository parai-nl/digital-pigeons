// Package duif is de parai-core-module "Digitale Duif van Rotterdam":
// drie AI-duifpersona's die de stadsduif een stem geven in besluitvorming
// (Parai × AI-MAPS / EUR). Alle code, prompts en configuratie in dit package
// zijn nieuw geschreven projectcode (MIT); parai-core wordt uitsluitend als
// dependency geïmporteerd.
package duif

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/parai-nl/ai-maps-duiven/module/duif/soniox"
	"gopkg.in/yaml.v3"
)

//go:embed config
var configFS embed.FS

// idPattern is de core-naamregel voor URL-safe identifiers.
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// promptLangs zijn de verplichte taalvarianten per persona: Engels en Nederlands.
var promptLangs = []string{"en", "nl"}

// LocalName is een tweetalige weergavenaam (EN eerst, NL als optie).
type LocalName struct {
	EN string `yaml:"en" json:"en"`
	NL string `yaml:"nl" json:"nl"`
}

// Topic is één thema waarop een duif in de vergadering aanslaat. Het label is
// wat de vergadering ziet; de beschrijving is wat de topic-classifier leest.
// Beide tweetalig.
//
// Gevoeligheid (0–1, exclusief 0) verschuift de interventiedrempel per thema:
// 0,5 is neutraal, hoger laat de duif er eerder op aanslaan.
type Topic struct {
	researchBoundary string    // internal catalogue constraint; never sent as a signal explanation
	ID               string    `yaml:"id"`
	Label            LocalName `yaml:"label"`
	Beschrijving     LocalName `yaml:"beschrijving"`
	Gevoeligheid     float64   `yaml:"gevoeligheid"`
}

// LabelFor geeft het weergavelabel in de gevraagde taal.
func (t Topic) LabelFor(lang string) string { return pickLang(t.Label, lang) }

// DescribeFor geeft de classifier-beschrijving in de gevraagde taal.
func (t Topic) DescribeFor(lang string) string { return pickLang(t.Beschrijving, lang) }

func pickLang(n LocalName, lang string) string {
	if normLang(lang) == "nl" {
		return n.NL
	}
	return n.EN
}

// naamVanID geeft de weergavenaam van de duif met dit id, in de gevraagde
// taal. Geen enkele prompt in Go legt een duivennaam vast: wie een
// andere duif bij naam moet noemen, haalt hem hier op. Kent de set het id
// niet — of is er geen set, zoals in een kale prompt-test — dan blijft het id
// over, want dat is het enige wat de code zelf zeker weet.
func (s *PersonaSet) naamVanID(id, lang string) string {
	if s != nil {
		if p, ok := s.Get(id); ok {
			return pickLang(p.Naam, lang)
		}
	}
	return id
}

// inZin geeft de vorm van een naam zoals die middenin een zin hoort. Het
// Nederlands schrijft zo'n rolnaam achter een lidwoord klein ("de
// kennispartner"); het Engels laat hem staan zoals personas.yaml hem schrijft
// ("the Knowledge Partner").
func inZin(naam, lang string) string {
	if normLang(lang) != "nl" || naam == "" {
		return naam
	}
	r := []rune(naam)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// SoundBand is de arousal-band waarin een veldopname past:
// de non-verbale duif koert rustig, koert nadrukkelijk, of fladdert en slaat
// alarm. De waarden zijn Engels — één naamruimte voor config, code en wire.
type SoundBand string

const (
	BandCalm      SoundBand = "calm"
	BandInsistent SoundBand = "insistent"
	BandAlarm     SoundBand = "alarm"
)

// soundBands is de oplopende bandvolgorde; de bandkeuze gebruikt hem om terug
// te vallen op een naburige band als de eigen band leeg is.
var soundBands = []SoundBand{BandCalm, BandInsistent, BandAlarm}

// validBand zegt of een band bestaat.
func validBand(b SoundBand) bool {
	for _, known := range soundBands {
		if b == known {
			return true
		}
	}
	return false
}

// Sound is één veldopname van de non-verbale duif: een mp3 in de
// config-FS, geannoteerd met de arousal-band waarin hij past. De bestanden in
// config/sounds/ zijn placeholders tot de opnamedag.
type Sound struct {
	ID         string    `yaml:"id" json:"id"`
	Bestand    string    `yaml:"bestand" json:"-"`
	Band       SoundBand `yaml:"band" json:"band"`
	DurationMs int       `yaml:"duration_ms,omitempty" json:"durationMs,omitempty"`
}

// Motoren van de chat (`engine:` in personas.yaml). De keuze staat bij de
// duif en niet bij de route: dezelfde POST /v1/chat/turn bedient beide.
//
// Let op het woord: "engine" betekent in deze module twee dingen. Hier is het
// de chatmotor (Persona.Engine, chatEngine in turn.go); meetingEngine is de
// orkestrator van de vergaderzaal, en die negeert dit veld juist — aan tafel
// spreekt elke duif rechtstreeks.
const (
	// EnginePipeline is de directe spreekbeurt: retrieval → prompt →
	// CompleteStream. De kennispartner praat zo, en het is de default.
	EnginePipeline = "pipeline"
	// EngineAgent zet een facilitator vóór de duif (pkg/agent.RunStream met
	// een tool-lus). De duif antwoordt dan als artifact binnen de beurt.
	EngineAgent = "agent"
)

// Persona is één geconfigureerde duif uit personas.yaml.
type Persona struct {
	ID      string    `yaml:"id"`
	Naam    LocalName `yaml:"naam"`
	VoiceID string    `yaml:"voice_id"`
	// Spreektempo is de tempo-nuance van deze duif. De stem is bewust er één
	// voor beide pratende duiven; wat per duif verschilt is hoe snel zij praat.
	// 0 (of een ontbrekend veld) is het normale tempo van de dienst. De
	// Soniox-TTS accepteert 0,7–1,3.
	Spreektempo float64           `yaml:"spreektempo"`
	PromptFiles map[string]string `yaml:"prompts"` // taal → pad binnen de config-FS
	Kennisbank  bool              `yaml:"kennisbank"`
	Chat        bool              `yaml:"chat"`
	Vergadering bool              `yaml:"vergadering"`
	// InterventieZelfstandig is de standaardstand van zelfstandig meedoen
	// voor déze duif. Nil betekent: de gedeelde stand uit `gedrag:`.
	// Een pointer en geen bool, omdat "niet ingevuld" en "uitdrukkelijk uit"
	// hier twee verschillende dingen zijn — anders zou elke duif zonder eigen
	// stand de gedeelde stand overschrijven met false.
	//
	// De stand verschilt per duif omdat de duiven verschillen: de
	// signalerende duif heeft geen woorden en valt zonder zelfstandig meedoen
	// helemaal stil, de pratende twee onderbreken dan een gesprek met een
	// alinea. Waar het om gaat blijft overeind: de knop is de gegarandeerde
	// basis en de vergadering mag de stand altijd verzetten.
	InterventieZelfstandig *bool `yaml:"interventie_zelfstandig"`
	// Engine kiest de chatmotor: EnginePipeline (default, ook bij een lege
	// waarde) of EngineAgent. De vergadermodus negeert dit veld: aan tafel
	// spreekt elke duif rechtstreeks.
	Engine string `yaml:"engine"`
	// FacilitatorRole zegt hoeveel de facilitator rond déze duif hardop doet.
	// Nil is de stille stand: geen kader, geen bemiddeling. De pipeline-duif
	// kent hem niet; hij hoort bij EngineAgent.
	FacilitatorRole     *FacilitatorRole `yaml:"facilitator"`
	SignalCatalogue     string           `yaml:"signal_catalogue"`
	signalResearchReady bool
	// Spreeklat is de eigen lat van deze duif voor ongevraagd spreken:
	// de instructie die in de kandidaatprompt landt plus de getallen waarop de
	// klok beslist. Verplicht voor elke vergaderduif.
	Spreeklat *SpeakBar `yaml:"spreeklat"`
	Topics    []Topic   `yaml:"topics"`   // thema's; verplicht voor vergaderduiven
	Geluiden  []Sound   `yaml:"geluiden"` // geluidenset; verplicht voor de non-verbale duif

	prompts map[string]string // taal → geladen promptinhoud (verlaat de server nooit)
}

// FacilitatorRole is de zichtbare rol van de facilitator rond één duif
// (personas.yaml, blok `facilitator:` per duif). Twee losse schakelaars, want
// het zijn twee momenten: het kader vooraf reist mee als argument van
// ask_pigeon en kost geen extra modelronde, de bemiddeling achteraf kost er
// precies één. Wie alleen het goedkope moment wil, zet alleen dat aan.
type FacilitatorRole struct {
	// Framing laat de facilitator één zin aan de mensen schrijven vóór hij de
	// duif het woord geeft.
	Framing bool `yaml:"framing"`
	// Mediation geeft hem na een geslaagde ask_pigeon één afsluitende beurt
	// zonder tools, waarin hij het standpunt van de duif aan het gesprek van
	// de mensen koppelt.
	Mediation bool `yaml:"mediation"`
}

// FacilitatorFraming zegt of deze duif een kader vooraf krijgt.
func (p *Persona) FacilitatorFraming() bool {
	return p.FacilitatorRole != nil && p.FacilitatorRole.Framing
}

// FacilitatorMediation zegt of deze duif een bemiddeling achteraf krijgt.
func (p *Persona) FacilitatorMediation() bool {
	return p.FacilitatorRole != nil && p.FacilitatorRole.Mediation
}

// AutonomousDefault geeft de standaardstand van zelfstandig meedoen voor deze
// duif: haar eigen stand wanneer personas.yaml die noemt, anders de gedeelde
// stand uit `gedrag:`. Dit is een startwaarde, geen slot — de vergadering zet
// de schakelaar om wanneer zij wil.
func (p *Persona) AutonomousDefault(shared bool) bool {
	if p != nil && p.InterventieZelfstandig != nil {
		return *p.InterventieZelfstandig
	}
	return shared
}

// SignalsOnly zegt of deze duif alleen signaleert: zij zit aan tafel, maar
// schrijft geen bijdrage — haar hele antwoord is de kopregel en wat volgt is
// een geluid. Het is dezelfde eigenschap als "praat niet in de chat", en juist
// omdat er drie plekken op afgaan (de kandidaatprompt, het venster en het
// tokenbudget van de kopregel) verdient zij één naam in plaats van een `!Chat`
// dat de lezer elke keer opnieuw moet uitleggen.
func (p *Persona) SignalsOnly() bool { return p != nil && !p.Chat }

// SpeakingSpeed geeft het spreektempo van deze duif, of 0 als zij er geen
// heeft — dan spreekt de provider op zijn eigen tempo. Eén plek die dat
// antwoordt, net als resolveVoiceID voor de stem.
func (p *Persona) SpeakingSpeed() float64 {
	if p == nil {
		return 0
	}
	return p.Spreektempo
}

// EngineOr geeft de motor van deze duif, met de pipeline als default.
func (p *Persona) EngineOr() string {
	if p.Engine == "" {
		return EnginePipeline
	}
	return p.Engine
}

// Facilitator is de stem die met de mens praat wanneer een duif op
// EngineAgent staat: hij interpreteert de vraag, roept de duif aan en vat
// samen. Bewust géén Persona — hij is geen duif, staat niet in /v1/duiven,
// heeft geen stem, geen thema's en geen vergaderrol. Daarom een eigen,
// minimaal configblok in plaats van een vierde rij met vier uitzonderingen.
// Bewust zonder naam: de naam die de gebruiker ziet komt uit de frontend-i18n
// (`chat.facilitator`), en een tweede bron voor dezelfde string — met een
// fail-fast-validatie op een veld dat nooit gerenderd wordt — is er één te
// veel.
type Facilitator struct {
	PromptFiles map[string]string `yaml:"prompts"` // taal → pad binnen de config-FS

	prompts map[string]string // taal → geladen promptinhoud (verlaat de server nooit)
}

// Prompt geeft de facilitator-systemprompt in de gevraagde taal.
func (f *Facilitator) Prompt(lang string) string { return f.prompts[normLang(lang)] }

// Prompt geeft de systemprompt in de gevraagde taal ("" als die taal niet bestaat).
func (p *Persona) Prompt(lang string) string { return p.prompts[lang] }

// Topic zoekt een thema van deze duif op id.
func (p *Persona) Topic(id string) (Topic, bool) {
	for _, t := range p.Topics {
		if t.ID == id {
			return t, true
		}
	}
	return Topic{}, false
}

// SpeakBar is de spreeklat van één duif: wanneer zij uit zichzelf
// invalt. Hij bestaat uit twee helften die niet zonder elkaar kunnen. De
// instructie is wat het model leest wanneer het beslist of er een kandidaat
// komt — de kennispartner verdient haar beurt met een toevoeging, de
// stakeholder spreekt zodra het haar aangaat, de signalerende heeft de
// laagste lat van de drie. De getallen zijn wat de klok daarna doet: hoe hoog
// de score moet zijn, hoe lang de duif daarna zwijgt, en of zij op een
// zinseinde wacht.
//
// De lat staat per duif en niet in de gedeelde gedragsconfig, omdat het
// verschil tussen de drie duiven juist hier zit.
type SpeakBar struct {
	PromptFiles map[string]string `yaml:"prompts"` // taal → pad binnen de config-FS
	// Drempel is de score die een kandidaat moet halen om vanzelf te klinken.
	Drempel float64 `yaml:"drempel"`
	// CooldownSeconden is de rust na een eigen bijdrage.
	CooldownSeconden int `yaml:"cooldown_seconden"`
	// CooldownGroeiSeconden laat die rust per bijdrage oplopen: de rem die in
	// de plaats komt van een sessieplafond. 0 = een vaste cooldown.
	CooldownGroeiSeconden int `yaml:"cooldown_groei_seconden"`
	// CooldownMaxSeconden begrenst die oplopende rust; 0 = geen bovengrens.
	CooldownMaxSeconden int `yaml:"cooldown_max_seconden"`
	// ThemaCooldownSeconden houdt de duif van haar stokpaardje af; 0 = geen.
	ThemaCooldownSeconden int `yaml:"thema_cooldown_seconden"`
	// Beleefdheid laat de duif op een zinseinde en op stilte wachten. Hij
	// staat bij alle drie de duiven op false: wachten op een gaatje
	// betekende in de praktijk zwijgen. De knop blijft bestaan voor wie het
	// wachten terug wil zetten; met false grijpt `stilte_milliseconden`
	// nergens meer in.
	Beleefdheid bool `yaml:"beleefdheid"`
	// ZonderBron zegt of deze duif ook zonder passende bron iets mag zeggen.
	// De kennispartner staat op false: zonder bron heeft zij niets toe te
	// voegen en kost een generatie alleen geld. De
	// stakeholder staat op true: zij mag een begrensd standpunt
	// innemen en benoemt zelf dat er geen onderzoek voor is.
	ZonderBron bool `yaml:"zonder_bron"`

	prompts map[string]string // taal → geladen instructie (verlaat de server nooit)
}

// Instruction geeft de spreeklat-instructie in de gevraagde taal.
func (b *SpeakBar) Instruction(lang string) string {
	if b == nil {
		return ""
	}
	return b.prompts[normLang(lang)]
}

// Cooldown is de rust na `spoken` eigen bijdragen in deze sessie: de
// basisrust plus de groei van elke bijdrage daarna, begrensd op het maximum.
func (b *SpeakBar) Cooldown(spoken int) time.Duration {
	if b == nil {
		return 0
	}
	seconds := b.CooldownSeconden
	if spoken > 1 && b.CooldownGroeiSeconden > 0 {
		seconds += (spoken - 1) * b.CooldownGroeiSeconden
	}
	if b.CooldownMaxSeconden > 0 && seconds > b.CooldownMaxSeconden {
		seconds = b.CooldownMaxSeconden
	}
	return time.Duration(seconds) * time.Second
}

// TopicCooldown is de rust op één thema; 0 betekent: geen themacooldown.
func (b *SpeakBar) TopicCooldown() time.Duration {
	if b == nil {
		return 0
	}
	return time.Duration(b.ThemaCooldownSeconden) * time.Second
}

// Polite zegt of deze duif op een zinseinde en stilte wacht. Geen enkele duif
// in de configuratie zegt nog ja; de klok houdt alleen de lat en
// de cooldown over.
func (b *SpeakBar) Polite() bool { return b != nil && b.Beleefdheid }

// WithoutSources zegt of deze duif ook zonder passende bron mag spreken.
func (b *SpeakBar) WithoutSources() bool { return b != nil && b.ZonderBron }

// Threshold is de spreeklat van deze duif voor één thema en één
// gretigheidsstand: de eigen drempel, verschoven door de gevoeligheid van het
// thema en door wat de vergadering aan gretigheid koos.
func (b *SpeakBar) Threshold(eagerness string, gevoeligheid float64) float64 {
	base := 0.5
	if b != nil {
		base = b.Drempel
	}
	return clampFloat(base+eagernessShift[normEagerness(eagerness)]-(gevoeligheid-0.5)*sensitivitySpread,
		thresholdFloor, thresholdCeiling)
}

// Gedrag is de gedragsconfig. De detectie-knoppen zijn startwaarden; kalibreer
// ze op eval/meeting-conversations.v1.yaml.
type Gedrag struct {
	InterventieZelfstandig      bool `yaml:"interventie_zelfstandig" json:"interventieZelfstandig"`
	InterventieThrottleSeconden int  `yaml:"interventie_throttle_seconden" json:"interventieThrottleSeconden"`
	// De drie klokken van de inactiviteitsbewaking, alle drie in
	// seconden: zo kan een test of een demo-opstelling ze kort zetten zonder
	// codewijziging. Ze staan niet in de interface; wijzigen is een besluit.
	InactiviteitPromptSeconden int    `yaml:"inactiviteit_prompt_seconden" json:"inactiviteitPromptSeconden"`
	InactiviteitSluitSeconden  int    `yaml:"inactiviteit_sluit_seconden" json:"inactiviteitSluitSeconden"`
	VergaderingMaxSeconden     int    `yaml:"vergadering_max_seconden" json:"vergaderingMaxSeconden"`
	TaalDefault                string `yaml:"taal_default" json:"taalDefault"`

	DetectieGretigheid       string `yaml:"detectie_gretigheid" json:"detectieGretigheid"`
	DetectieVensterWoorden   int    `yaml:"detectie_venster_woorden" json:"detectieVensterWoorden"`
	DetectieIntervalSeconden int    `yaml:"detectie_interval_seconden" json:"detectieIntervalSeconden"`
	StilteMilliseconden      int    `yaml:"stilte_milliseconden" json:"stilteMilliseconden"`
	ArousalHalveringSeconden int    `yaml:"arousal_halvering_seconden" json:"arousalHalveringSeconden"`
}

// Gretigheidsstanden. De waarden zijn Engels: één naamruimte voor config, code
// en wire.
const (
	EagernessReserved = "reserved"
	EagernessNormal   = "normal"
	EagernessEager    = "eager"
)

// eagernessShift verschuift de spreeklat van élke duif met dezelfde stap. De
// lat zelf staat per duif (SpeakBar.Drempel); deze knop zegt alleen of de
// vergadering vandaag een terughoudender of gretiger duif wil.
var eagernessShift = map[string]float64{
	EagernessReserved: 0.15,
	EagernessNormal:   0,
	EagernessEager:    -0.15,
}

// PersonaSet is de gevalideerde persona-configuratie.
type PersonaSet struct {
	Personas []*Persona
	Gedrag   Gedrag
	// Facilitator is het configblok van de facilitator-stem. Nil wanneer geen
	// enkele duif op EngineAgent staat; LoadPersonas weigert die combinatie
	// wél als er een agent-duif is.
	Facilitator *Facilitator

	byID    map[string]*Persona
	sounds  map[string]Sound // geluid-id → geluid, over alle persona's heen
	soundFS fs.FS            // waar de veldopnames vandaan komen
}

// Get zoekt een persona op id.
func (s *PersonaSet) Get(id string) (*Persona, bool) {
	p, ok := s.byID[id]
	return p, ok
}

// OpenSound opent het bestand bij een geluid-id. De aanroeper sluit het.
func (s *PersonaSet) OpenSound(id string) (fs.File, error) {
	snd, ok := s.sounds[id]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return s.soundFS.Open(snd.Bestand)
}

// configDoc is het yaml-wortelschema van personas.yaml.
type configDoc struct {
	Personas    []*Persona   `yaml:"personas"`
	Gedrag      Gedrag       `yaml:"gedrag"`
	Facilitator *Facilitator `yaml:"facilitator"`
}

// LoadPersonas laadt en valideert personas.yaml + promptbestanden uit fsys.
// Validatie is fail-fast: een ontbrekende of lege
// promptfile, een duplicaat- of niet-URL-safe id of een ontbrekende
// taalvariant is een bootfout, geen runtime-verrassing.
func LoadPersonas(fsys fs.FS) (*PersonaSet, error) {
	raw, err := fs.ReadFile(fsys, "personas.yaml")
	if err != nil {
		return nil, fmt.Errorf("duif: personas.yaml lezen: %w", err)
	}
	var doc configDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("duif: personas.yaml parsen: %w", err)
	}
	if len(doc.Personas) == 0 {
		return nil, fmt.Errorf("duif: personas.yaml bevat geen personas")
	}

	set := &PersonaSet{
		Personas: doc.Personas,
		Gedrag:   doc.Gedrag,
		byID:     make(map[string]*Persona, len(doc.Personas)),
		sounds:   map[string]Sound{},
		soundFS:  fsys,
	}
	for _, p := range doc.Personas {
		if !idPattern.MatchString(p.ID) {
			return nil, fmt.Errorf("duif: persona-id %q is niet URL-safe (%s)", p.ID, idPattern)
		}
		if _, dup := set.byID[p.ID]; dup {
			return nil, fmt.Errorf("duif: duplicaat persona-id %q", p.ID)
		}
		if p.Naam.EN == "" || p.Naam.NL == "" {
			return nil, fmt.Errorf("duif: persona %q mist een naam (en=%q, nl=%q)", p.ID, p.Naam.EN, p.Naam.NL)
		}
		if p.Spreektempo != 0 && !(p.Spreektempo >= soniox.MinSpeed && p.Spreektempo <= soniox.MaxSpeed) {
			// Fail-fast: een tempo buiten het bereik weigert de TTS-dienst met
			// invalid_request, en dat zou pas bij de eerste spreekbeurt blijken.
			//
			// De voorwaarde staat bewust als "niet binnen het bereik" en niet
			// als twee losse vergelijkingen: `spreektempo: .nan` is niet 0 en
			// ligt noch onder MinSpeed noch boven MaxSpeed, want élke
			// vergelijking met NaN is onwaar. Zo'n waarde kwam er ongezien
			// doorheen en sloopte daarna elke spreekbeurt, omdat NaN niet in
			// JSON past en het openingsbericht dus niet te versturen is.
			return nil, fmt.Errorf("duif: persona %q heeft spreektempo %v, want 0 (het tempo van de dienst) of %v–%v",
				p.ID, p.Spreektempo, soniox.MinSpeed, soniox.MaxSpeed)
		}
		if p.Engine != "" && p.Engine != EnginePipeline && p.Engine != EngineAgent {
			return nil, fmt.Errorf("duif: persona %q heeft engine %q, want %s of %s",
				p.ID, p.Engine, EnginePipeline, EngineAgent)
		}
		if p.FacilitatorRole != nil && p.EngineOr() != EngineAgent {
			return nil, fmt.Errorf("duif: persona %q heeft een facilitator-blok maar draait op engine %s (de facilitator bestaat alleen op %s)",
				p.ID, p.EngineOr(), EngineAgent)
		}
		prompts, err := loadPrompts(fsys, "persona "+p.ID, p.PromptFiles)
		if err != nil {
			return nil, err
		}
		p.prompts = prompts
		if err := loadSignalCatalogue(fsys, p); err != nil {
			return nil, err
		}
		if err := validateTopics(p); err != nil {
			return nil, err
		}
		if err := loadSpeakBar(fsys, p); err != nil {
			return nil, err
		}
		if err := set.loadSounds(fsys, p); err != nil {
			return nil, err
		}
		set.byID[p.ID] = p
	}
	if g := doc.Gedrag.DetectieGretigheid; g != "" {
		if _, ok := eagernessShift[g]; !ok {
			return nil, fmt.Errorf("duif: detectie_gretigheid %q is geen bekende stand (%s, %s, %s)",
				g, EagernessReserved, EagernessNormal, EagernessEager)
		}
	}
	if err := set.loadFacilitator(fsys, doc.Facilitator); err != nil {
		return nil, err
	}
	return set, nil
}

// loadFacilitator laadt en valideert het facilitator-blok. Ontbreekt het
// terwijl een duif op EngineAgent staat, dan boot de module niet: die duif
// zou anders pas bij de eerste chatbeurt zonder stem blijken te zitten
// (fail-fast).
func (s *PersonaSet) loadFacilitator(fsys fs.FS, f *Facilitator) error {
	if f == nil {
		for _, p := range s.Personas {
			if p.EngineOr() == EngineAgent {
				return fmt.Errorf("duif: persona %q draait op engine %s, maar personas.yaml heeft geen facilitator-blok", p.ID, EngineAgent)
			}
		}
		return nil
	}
	prompts, err := loadPrompts(fsys, "facilitator", f.PromptFiles)
	if err != nil {
		return err
	}
	f.prompts = prompts
	s.Facilitator = f
	return nil
}

// loadPrompts leest de promptbestanden per taal en eist beide taalvarianten
// (EN én NL). `wat` benoemt de eigenaar in de foutmelding.
func loadPrompts(fsys fs.FS, wat string, files map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(promptLangs))
	for _, lang := range promptLangs {
		path, ok := files[lang]
		if !ok || path == "" {
			return nil, fmt.Errorf("duif: %s mist de %s-promptfile (EN én NL verplicht)", wat, lang)
		}
		body, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, fmt.Errorf("duif: %s: promptfile %s: %w", wat, path, err)
		}
		if strings.TrimSpace(string(body)) == "" {
			return nil, fmt.Errorf("duif: %s: promptfile %s is leeg", wat, path)
		}
		out[lang] = string(body)
	}
	return out, nil
}

// validateTopics bewaakt de themalijst. Een vergaderduif zonder thema's kan
// nooit aanslaan — dat is een configuratiefout, geen stille eigenschap, dus
// fail-fast bij het booten.
func validateTopics(p *Persona) error {
	if p.Vergadering && len(p.Topics) == 0 {
		return fmt.Errorf("duif: vergaderduif %q heeft geen topics (zij kan dan nooit aanslaan)", p.ID)
	}
	seen := make(map[string]bool, len(p.Topics))
	for _, t := range p.Topics {
		if !idPattern.MatchString(t.ID) {
			return fmt.Errorf("duif: persona %q: topic-id %q is niet URL-safe (%s)", p.ID, t.ID, idPattern)
		}
		if seen[t.ID] {
			return fmt.Errorf("duif: persona %q: duplicaat topic-id %q", p.ID, t.ID)
		}
		seen[t.ID] = true
		if t.Label.EN == "" || t.Label.NL == "" {
			return fmt.Errorf("duif: persona %q: topic %q mist een label (EN én NL verplicht)", p.ID, t.ID)
		}
		if strings.TrimSpace(t.Beschrijving.EN) == "" || strings.TrimSpace(t.Beschrijving.NL) == "" {
			return fmt.Errorf("duif: persona %q: topic %q mist een beschrijving (de classifier leest die)", p.ID, t.ID)
		}
		if t.Gevoeligheid <= 0 || t.Gevoeligheid > 1 {
			return fmt.Errorf("duif: persona %q: topic %q heeft gevoeligheid %v, want een waarde in (0,1]",
				p.ID, t.ID, t.Gevoeligheid)
		}
	}
	return nil
}

// loadSpeakBar laadt en valideert de spreeklat van één vergaderduif.
// Fail-fast: een vergaderduif zonder lat zou stilletjes op een gedeelde
// standaard terugvallen, en juist het verschil tussen de drie duiven is hier
// het ontwerp.
func loadSpeakBar(fsys fs.FS, p *Persona) error {
	if !p.Vergadering {
		if p.Spreeklat != nil {
			return fmt.Errorf("duif: persona %q heeft een spreeklat maar zit niet aan tafel", p.ID)
		}
		return nil
	}
	if p.Spreeklat == nil {
		return fmt.Errorf("duif: vergaderduif %q heeft geen spreeklat (de lat verschilt per duif)", p.ID)
	}
	b := p.Spreeklat
	if b.Drempel <= 0 || b.Drempel > 1 {
		return fmt.Errorf("duif: persona %q: spreeklat-drempel %v, want een waarde in (0,1]", p.ID, b.Drempel)
	}
	if b.CooldownSeconden <= 0 {
		return fmt.Errorf("duif: persona %q: spreeklat mist een cooldown", p.ID)
	}
	if b.CooldownGroeiSeconden < 0 || b.CooldownMaxSeconden < 0 || b.ThemaCooldownSeconden < 0 {
		return fmt.Errorf("duif: persona %q: spreeklat heeft een negatieve cooldown", p.ID)
	}
	if b.CooldownMaxSeconden > 0 && b.CooldownMaxSeconden < b.CooldownSeconden {
		return fmt.Errorf("duif: persona %q: spreeklat-maximum ligt onder de basiscooldown", p.ID)
	}
	prompts, err := loadPrompts(fsys, "spreeklat "+p.ID, b.PromptFiles)
	if err != nil {
		return err
	}
	b.prompts = prompts
	return nil
}

// loadSounds valideert en registreert de geluidenset van één duif. Een
// vergaderduif die niet praat kán alleen geluid maken: geen set is dan een
// configuratiefout, geen stille eigenschap.
func (s *PersonaSet) loadSounds(fsys fs.FS, p *Persona) error {
	if p.Vergadering && !p.Chat && len(p.Geluiden) == 0 {
		return fmt.Errorf("duif: non-verbale vergaderduif %q heeft geen geluiden (zij kan dan niets laten horen)", p.ID)
	}
	for _, snd := range p.Geluiden {
		if snd.DurationMs != 0 && (snd.DurationMs < 200 || snd.DurationMs > 15000) {
			return fmt.Errorf("duif: geluid %q: ongeldige duur", snd.ID)
		}
		if !idPattern.MatchString(snd.ID) {
			return fmt.Errorf("duif: persona %q: geluid-id %q is niet URL-safe (%s)", p.ID, snd.ID, idPattern)
		}
		if _, dup := s.sounds[snd.ID]; dup {
			return fmt.Errorf("duif: duplicaat geluid-id %q", snd.ID)
		}
		if !validBand(snd.Band) {
			return fmt.Errorf("duif: persona %q: geluid %q heeft band %q, want %s, %s of %s",
				p.ID, snd.ID, snd.Band, BandCalm, BandInsistent, BandAlarm)
		}
		if _, err := fs.Stat(fsys, snd.Bestand); err != nil {
			return fmt.Errorf("duif: persona %q: geluid %q verwijst naar %s: %w", p.ID, snd.ID, snd.Bestand, err)
		}
		s.sounds[snd.ID] = snd
	}
	return nil
}

// LoadEmbeddedPersonas laadt de meegecompileerde configuratie uit config/.
func LoadEmbeddedPersonas() (*PersonaSet, error) {
	sub, err := fs.Sub(configFS, "config")
	if err != nil {
		return nil, fmt.Errorf("duif: config-FS: %w", err)
	}
	set, err := LoadPersonas(sub)
	if err != nil {
		return nil, err
	}
	if err := applySoundManifest(sub, set); err != nil {
		return nil, err
	}
	return set, nil
}
