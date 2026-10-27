package web

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"log"
	"strings"
)

// Своя карточка custom:energy-schema-gauge (gaugecard.js) — обычный гейдж HA с
// подменённой подписью значения («1 д 19 ч»). Аддон кладёт модуль в
// /config/www и держит ресурс дашбордов /local/energy-schema-gauge.js?v=<хеш>
// (новый хеш — браузеры берут свежий файл после обновления аддона).
//
//go:embed gaugecard.js
var gaugeCardJS []byte

const gaugeCardFile = "energy-schema-gauge.js"

func gaugeCardURL() string {
	h := sha256.Sum256(gaugeCardJS)
	return fmt.Sprintf("/local/%s?v=%x", gaugeCardFile, h[:6])
}

// ensureGaugeCard пишет модуль и создаёт/обновляет ресурс; false — не удалось
// (тогда карточку на дашборд не ставим: HA показал бы «custom element doesn't exist»).
func (s *Server) ensureGaugeCard() bool {
	if err := writeAtomic(wwwDir+"/"+gaugeCardFile, gaugeCardJS); err != nil {
		log.Printf("gauge card: write: %v", err)
		return false
	}
	res, err := s.client.LovelaceResources()
	if err != nil {
		log.Printf("gauge card: resources: %v", err)
		return false
	}
	url, id := gaugeCardURL(), ""
	for _, r := range res {
		if strings.HasPrefix(r.URL, "/local/"+gaugeCardFile) {
			if r.URL == url {
				return true
			}
			id = r.ID
		}
	}
	if err := s.client.SaveLovelaceResource(id, url); err != nil {
		log.Printf("gauge card: save resource: %v", err)
		return false
	}
	log.Printf("gauge card: resource %s", url)
	return true
}
