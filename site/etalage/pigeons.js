// De drie duiven van de etalage. Site-id's zijn de publieksnamen; tussen haakjes
// de module-id: kennispartner (biological), stakeholder (individualist),
// signalerend (nonverbal). Namen en teksten staan in i18n.js, beeld in het
// manifest van de beeldstroom of — als dat niet laadt — in de portretten.

// Volgorde van de waaier. De Kennispartner staat bij binnenkomst vooraan, met
// links de Signalerende duif en rechts de Stakeholder.
export const ORDER = ['signalerend', 'kennispartner', 'stakeholder'];
export const START = 'kennispartner';

export const MODES = {
  kennispartner: ['chat', 'meeting'],
  stakeholder: ['chat', 'meeting'],
  signalerend: ['meeting'],
};

export const TALKING = ['kennispartner', 'stakeholder'];

export const otherTalking = (id) => (id === 'kennispartner' ? 'stakeholder' : 'kennispartner');

// Terugval als het manifest van de beeldstroom niet laadt: het kaart- en
// kopportret van het v4-model, met alfakanaal. `focus` is de
// object-position waarmee het borstbeeld de kaart vult; de Signalerende duif
// kijkt naar links, dus haar snavel blijft in beeld.
export const FALLBACK = {
  kennispartner: {
    portrait: 'etalage/media/portretten/kennispartner.webp',
    head: 'etalage/media/portretten/kennispartner-kop.webp',
    focus: '50% 100%',
  },
  stakeholder: {
    portrait: 'etalage/media/portretten/stakeholder.webp',
    head: 'etalage/media/portretten/stakeholder-kop.webp',
    focus: '50% 100%',
  },
  signalerend: {
    portrait: 'etalage/media/portretten/signalerend.webp',
    head: 'etalage/media/portretten/signalerend-kop.webp',
    focus: '15% 100%',
  },
};

// De naamsvermelding van de 3D-duif, als het manifest geen eigen `credits` heeft.
export const FALLBACK_CREDITS = [
  {
    nl: '3D-duif: Digitale Duiven, fototextuur Luc Viatour / Lucnix.be · CC BY-SA 3.0',
    en: '3D pigeon: Digitale Duiven, photo texture by Luc Viatour / Lucnix.be · CC BY-SA 3.0',
    url: 'https://commons.wikimedia.org/wiki/File:Columba_livia_Luc_Viatour.jpg',
    license: 'CC BY-SA 3.0',
    licenseUrl: 'https://creativecommons.org/licenses/by-sa/3.0/',
  },
];
