package duif

import "github.com/parai-nl/parai-core/data"

// rowID leest de node-id uit een rij van GetNode of ListNodes.
//
// De id komt niet altijd als hetzelfde Go-type terug: de ene leesweg levert
// data.NodeID, de andere een gewone string. Een assertie op één van beide
// vormen faalt stil op de andere — en een lege id betekent hier dat de knoop
// wegvalt uit de graaf, de telling en het wissen. Deze functie is daarom de
// enige plek in de module die de rauwe `id` uit een rij haalt.
func rowID(row map[string]any) string {
	switch v := row["id"].(type) {
	case data.NodeID:
		return string(v)
	case string:
		return v
	default:
		return ""
	}
}
