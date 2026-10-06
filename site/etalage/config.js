// Instellingen van de etalage die zonder herbouw wisselen. Eén regel per ding
// dat later komt of wisselt: de thesislink, de uitlegvideo, de posters en kaartjes.
//
// Paden zijn relatief aan de siteroot (`site/`), zoals alle verwijzingen vanuit
// index.html. Links naar buiten zijn gewone links; de pagina zelf haalt nooit
// iets van een andere oorsprong op.
export const CONFIG = {
  // De werkomgeving (module op /duif). Ook het doel van "Bekijk in de kennisgraaf".
  loginUrl: 'https://pigeon.parai.nl/duif',

  // Openbare link naar de thesis, zodra die beschikbaar is. Zolang dit null
  // is, staat de titel er zonder link.
  thesisUrl: null,

  // Officiële AI-MAPS-pagina van de Erasmus Universiteit Rotterdam (alleen Engels).
  aboutUrl: 'https://www.eur.nl/en/research/research-groups-initiatives/artificial-intelligence-multi-agency-public-safety',

  // De uitlegvideo. Bijvoorbeeld:
  //   { src: 'etalage/media/video/uitleg.mp4', poster: 'etalage/media/video/uitleg.webp', vtt: { nl: 'etalage/media/video/uitleg.nl.vtt', en: 'etalage/media/video/uitleg.en.vtt' } }
  // `vtt` mag ook één pad zijn. Zolang dit null is, toont de band een stil beeld met "binnenkort".
  video: { src: 'etalage/media/video/uitleg.mp4', poster: 'etalage/media/video/uitleg.webp', vtt: { en: 'etalage/media/video/uitleg.en.vtt' }, width: 1440, height: 1080 },

  // Contact met het projectteam, voor toegang en voor wie een gat ziet: een
  // e-mailadres ('mailto:…') of een https-adres, of { href, label }. Zolang dit
  // null is, noemt de FAQ alleen het projectteam, zonder adres.
  contact: null,

  // De posters voor de AI-HHH-conferentie, in de volgorde van de teksten in
  // i18n.js (research.posters). `src` is het grote beeld voor de viewer, `thumb`
  // het kleine in de rij; één pad volstaat ook. null toont het kader met "binnenkort".
  posters: [
    { src: 'etalage/media/posters/poster-01.webp', thumb: 'etalage/media/posters/poster-01-klein.webp' },
    { src: 'etalage/media/posters/poster-02.webp', thumb: 'etalage/media/posters/poster-02-klein.webp' },
    { src: 'etalage/media/posters/poster-03.webp', thumb: 'etalage/media/posters/poster-03-klein.webp' },
  ],

  // Het materiaal voor groepssessies: de drukbestanden van de posters en de
  // kaartjes, om te downloaden. `bytes` is de echte omvang, voor de linktekst.
  downloads: {
    posters: { src: 'etalage/media/materiaal/posters-ai-hhh.pdf', bytes: 6741234 },
    cards: { src: 'etalage/media/materiaal/kaartjes-ai-hhh.pdf', bytes: 8155264 },
  },

  // De zes kaartjes, voor- en achterkant, in de volgorde van research.cards.
  // `landscape` voor de liggende kaartjes. Een kaartje zonder voorkant valt weg.
  cards: [
    { front: 'etalage/media/kaartjes/kaart-01-voor.webp', thumb: 'etalage/media/kaartjes/kaart-01-voor-klein.webp', back: 'etalage/media/kaartjes/kaart-01-achter.webp' },
    { front: 'etalage/media/kaartjes/kaart-02-voor.webp', thumb: 'etalage/media/kaartjes/kaart-02-voor-klein.webp', back: 'etalage/media/kaartjes/kaart-02-achter.webp' },
    { front: 'etalage/media/kaartjes/kaart-03-voor.webp', thumb: 'etalage/media/kaartjes/kaart-03-voor-klein.webp', back: 'etalage/media/kaartjes/kaart-03-achter.webp' },
    { front: 'etalage/media/kaartjes/kaart-04-voor.webp', thumb: 'etalage/media/kaartjes/kaart-04-voor-klein.webp', back: 'etalage/media/kaartjes/kaart-04-achter.webp' },
    { front: 'etalage/media/kaartjes/kaart-05-voor.webp', thumb: 'etalage/media/kaartjes/kaart-05-voor-klein.webp', back: 'etalage/media/kaartjes/kaart-05-achter.webp', landscape: true },
    { front: 'etalage/media/kaartjes/kaart-06-voor.webp', thumb: 'etalage/media/kaartjes/kaart-06-voor-klein.webp', back: 'etalage/media/kaartjes/kaart-06-achter.webp', landscape: true },
  ],
};
