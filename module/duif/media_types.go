package duif

import "mime"

// De uitlegvideo gaat als MP4 met WebVTT-ondertitels over de gewone
// module-assetroute van de core, een http.FileServer. Die kiest het
// Content-Type op extensie en valt terug op wat het besturingssysteem kent,
// en de core zet er nosniff naast. Een kale container kent .vtt niet; dan
// weigert een browser de ondertitels. Daarom legt de module beide typen vast.
func init() {
	for extension, mediaType := range map[string]string{".mp4": "video/mp4", ".vtt": "text/vtt; charset=utf-8"} {
		if err := mime.AddExtensionType(extension, mediaType); err != nil {
			panic(err)
		}
	}
}
