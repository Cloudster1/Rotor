package httpapi

import (
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/version"
)

// Product is how /meta names Rotor, which partners read to tell it from any other address.
const Product = "rotor"

// APIVersion rises only for a breaking change, as the suite's compatibility rules ask.
const APIVersion = "1"

// metaResponse names the product, its build and what it offers.
type metaResponse struct {
	Product    string       `json:"product"`
	Build      version.Info `json:"build"`
	APIVersion string       `json:"apiVersion"`
	// Capabilities are name@version, from the list the suite agrees in Dynamo, sorted.
	Capabilities []string `json:"capabilities"`
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, metaResponse{
		Product: Product, Build: version.Current(), APIVersion: APIVersion, Capabilities: capabilities(),
	})
}
