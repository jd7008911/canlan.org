package main

import (
	"fmt"
	"log"
	"net/http"

	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/yourproject/canglanfu-api/pkg/web"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
	}))

	// In-memory stores for dev/testing
	nonces := make(map[string]string)
	users := make(map[string]map[string]interface{})
	jwtSecret := []byte("dev-secret")

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/nonce", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Wallet string `json:"wallet"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Wallet == "" {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			// generate 32-byte hex nonce
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				http.Error(w, "failed to generate nonce", http.StatusInternalServerError)
				return
			}
			nonce := hex.EncodeToString(b)
			nonces[strings.ToLower(req.Wallet)] = nonce
			json.NewEncoder(w).Encode(map[string]string{"nonce": nonce})
		})

		r.Post("/auth/login", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Wallet    string `json:"wallet"`
				Signature string `json:"signature"`
				Referral  string `json:"referral"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Wallet == "" || req.Signature == "" {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			// check nonce exists
			walletKey := strings.ToLower(req.Wallet)
			nonce, ok := nonces[walletKey]
			if !ok {
				http.Error(w, "nonce expired or not found", http.StatusUnauthorized)
				return
			}

			// recover address from signature using Ethereum personal_sign scheme
			// prefixed message
			prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(nonce), nonce)
			hash := crypto.Keccak256([]byte(prefixed))

			sigHex := strings.TrimPrefix(req.Signature, "0x")
			sigBytes, err := hex.DecodeString(sigHex)
			if err != nil {
				http.Error(w, "invalid signature", http.StatusBadRequest)
				return
			}
			if len(sigBytes) != 65 {
				http.Error(w, "invalid signature length", http.StatusBadRequest)
				return
			}
			// adjust V if needed
			if sigBytes[64] >= 27 {
				sigBytes[64] -= 27
			}

			pubkey, err := crypto.SigToPub(hash, sigBytes)
			if err != nil {
				http.Error(w, "signature verification failed", http.StatusUnauthorized)
				return
			}
			recovered := crypto.PubkeyToAddress(*pubkey)
			if !strings.EqualFold(recovered.Hex(), req.Wallet) && !strings.EqualFold(recovered.Hex(), common.HexToAddress(req.Wallet).Hex()) {
				http.Error(w, "signature does not match wallet", http.StatusUnauthorized)
				return
			}

			// consume nonce
			delete(nonces, walletKey)

			// create or return fake user
			id := uuid.New()
			user := map[string]interface{}{
				"id":             id.String(),
				"wallet_address": req.Wallet,
				"referral_code":  "REF" + id.String()[:6],
			}
			users[walletKey] = user

			// create JWT
			claims := jwt.MapClaims{
				"user_id": user["id"],
				"wallet":  req.Wallet,
				"exp":     time.Now().Add(24 * time.Hour).Unix(),
				"iat":     time.Now().Unix(),
			}
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
			ss, err := token.SignedString(jwtSecret)
			if err != nil {
				http.Error(w, "failed to sign token", http.StatusInternalServerError)
				return
			}

			json.NewEncoder(w).Encode(map[string]interface{}{"token": ss, "user": user})
		})

		r.Get("/auth/me", func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			tokenStr := parts[1]
			tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) { return jwtSecret, nil })
			if err != nil || !tok.Valid {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			claims, _ := tok.Claims.(jwt.MapClaims)
			uid, _ := claims["user_id"].(string)
			wallet, _ := claims["wallet"].(string)
			// return stored user if exists
			if u, ok := users[strings.ToLower(wallet)]; ok {
				json.NewEncoder(w).Encode(u)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"id": uid, "wallet_address": wallet})
		})

		// --- Asset stub endpoints for local testing ---
		// In-memory sample data
		tokensList := []map[string]interface{}{
			{"symbol": "CGL", "name": "Canglanfu Token"},
			{"symbol": "ETH", "name": "Ethereum"},
		}
		prices := map[string]float64{"CGL": 0.42, "ETH": 1800.0}
		holders := map[string][]map[string]interface{}{
			"CGL": {
				{"address": "0x100", "balance": 100000},
				{"address": "0x200", "balance": 50000},
			},
		}

		// helper to parse bearer token and return user (from users map)
		parseUser := func(r *http.Request) (map[string]interface{}, error) {
			ah := r.Header.Get("Authorization")
			if ah == "" {
				return nil, http.ErrNoCookie
			}
			parts := strings.Split(ah, " ")
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				return nil, http.ErrNoCookie
			}
			tok, err := jwt.Parse(parts[1], func(t *jwt.Token) (interface{}, error) { return jwtSecret, nil })
			if err != nil || !tok.Valid {
				return nil, err
			}
			claims, _ := tok.Claims.(jwt.MapClaims)
			wallet, _ := claims["wallet"].(string)
			if u, ok := users[strings.ToLower(wallet)]; ok {
				return u, nil
			}
			return map[string]interface{}{"id": claims["user_id"], "wallet_address": wallet}, nil
		}

		r.Get("/assets/tokens", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(tokensList)
		})

		r.Get("/assets/tokens/{symbol}/price", func(w http.ResponseWriter, r *http.Request) {
			sym := strings.ToUpper(chi.URLParam(r, "symbol"))
			if p, ok := prices[sym]; ok {
				json.NewEncoder(w).Encode(map[string]interface{}{"symbol": sym, "price": p})
				return
			}
			http.Error(w, "not found", http.StatusNotFound)
		})

		r.Get("/assets/tokens/{symbol}/holders", func(w http.ResponseWriter, r *http.Request) {
			sym := strings.ToUpper(chi.URLParam(r, "symbol"))
			list, ok := holders[sym]
			if !ok {
				json.NewEncoder(w).Encode([]interface{}{})
				return
			}
			// support ?limit=
			limitStr := r.URL.Query().Get("limit")
			if limitStr != "" {
				if n, err := strconv.Atoi(limitStr); err == nil && n < len(list) {
					json.NewEncoder(w).Encode(list[:n])
					return
				}
			}
			json.NewEncoder(w).Encode(list)
		})

		r.Get("/assets/balance/{symbol}", func(w http.ResponseWriter, r *http.Request) {
			user, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			_ = user
			sym := strings.ToUpper(chi.URLParam(r, "symbol"))
			// fake balances
			bal := 1234.56
			json.NewEncoder(w).Encode(map[string]interface{}{"symbol": sym, "balance": bal})
		})

		r.Get("/assets/portfolio", func(w http.ResponseWriter, r *http.Request) {
			_, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			// return a simple portfolio
			portfolio := map[string]interface{}{
				"total_value_usd": 12345.67,
				"tokens": []map[string]interface{}{
					{"symbol": "CGL", "balance": 1000, "usd": 420.0},
					{"symbol": "ETH", "balance": 0.5, "usd": 900.0},
				},
			}
			json.NewEncoder(w).Encode(portfolio)
		})

		r.Get("/assets/stats/network", func(w http.ResponseWriter, r *http.Request) {
			stats := map[string]interface{}{"tvl_usd": 1234567.0, "unique_holders": 54321}
			json.NewEncoder(w).Encode(stats)
		})

		// --- Referral stub endpoints (auth required) ---
		// simple in-memory referral store keyed by user id
		referrals := make(map[string]map[string]interface{})
		referralHistory := make(map[string][]map[string]interface{})

		r.Get("/referral/code", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			if v, ok := referrals[uid]; ok {
				json.NewEncoder(w).Encode(map[string]string{"referral_code": v["code"].(string), "referral_link": v["link"].(string)})
				return
			}
			// default code
			code := "REF" + uid[:6]
			link := generateReferralLink(code)
			referrals[uid] = map[string]interface{}{"code": code, "link": link, "earnings": 0.0}
			json.NewEncoder(w).Encode(map[string]string{"referral_code": code, "referral_link": link})
		})

		r.Post("/referral/generate", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			newCode := "RC" + uuid.New().String()[:8]
			link := generateReferralLink(newCode)
			referrals[uid] = map[string]interface{}{"code": newCode, "link": link, "earnings": 0.0}
			json.NewEncoder(w).Encode(map[string]string{"referral_code": newCode, "referral_link": link})
		})

		r.Get("/referral/earnings", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			if v, ok := referrals[uid]; ok {
				json.NewEncoder(w).Encode(map[string]interface{}{"total_earnings": v["earnings"], "currency": "USDT"})
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"total_earnings": 0.0, "currency": "USDT"})
		})

		r.Get("/referral/stats", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			// fake node info
			node := map[string]interface{}{"DirectReferrals": 5, "TeamMembers": 42, "TeamPower": 1234}
			earnings := 0.0
			if v, ok := referrals[uid]; ok {
				if e, ok2 := v["earnings"].(float64); ok2 {
					earnings = e
				}
			}
			stats := map[string]interface{}{"direct_referrals": node["DirectReferrals"], "team_members": node["TeamMembers"], "team_power": node["TeamPower"], "total_earnings": earnings}
			json.NewEncoder(w).Encode(stats)
		})

		r.Get("/referral/history", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			limitStr := r.URL.Query().Get("limit")
			offsetStr := r.URL.Query().Get("offset")
			limit := 20
			offset := 0
			if limitStr != "" {
				if n, err := strconv.Atoi(limitStr); err == nil {
					limit = n
				}
			}
			if offsetStr != "" {
				if n, err := strconv.Atoi(offsetStr); err == nil {
					offset = n
				}
			}
			hist := referralHistory[uid]
			total := len(hist)
			end := offset + limit
			if end > total {
				end = total
			}
			if offset > total {
				offset = total
			}
			page := hist[offset:end]
			meta := map[string]interface{}{"limit": limit, "offset": offset, "total": total}
			json.NewEncoder(w).Encode(map[string]interface{}{"history": page, "meta": meta})
		})
	})

	log.Println("dev server starting on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatal(err)
	}
}

// generateReferralLink builds the full referral URL from the code.
func generateReferralLink(code string) string {
	return "https://www.canglanfu.org/?ref=" + code
}
