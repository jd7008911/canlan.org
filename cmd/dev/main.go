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

	"jd7008911/canlan.org/pkg/web"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

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
	purchases := make(map[string]map[string]interface{})
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
		// In-memory badges for dev/testing
		badgesList := []map[string]interface{}{
			{"id": "b1", "name": "Early Miner", "price_usd": 1.23, "description": "Awarded to early miners."},
			{"id": "b2", "name": "Whale", "price_usd": 12.00, "description": "Large holder badge."},
		}
		userBadges := make(map[string][]map[string]interface{})
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

		// --- Block reward stub endpoints (public + authenticated) ---
		// in-memory current block and per-user rewards
		currentBlock := map[string]interface{}{"id": "blk-1", "total_rewards": 1000, "distributed": false, "created_at": time.Now().UTC().Format(time.RFC3339)}
		userBlockRewards := make(map[string][]map[string]interface{})

		// Seed sample unclaimed rewards for the common test user (used in .http files)
		testUID := "ad96f6f5-f7a5-44b1-b417-d901dd080a24"
		userBlockRewards[testUID] = []map[string]interface{}{
			{"id": uuid.New().String(), "amount": 12.34, "block_id": "blk-1", "status": "unclaimed", "created_at": time.Now().UTC().Format(time.RFC3339)},
			{"id": uuid.New().String(), "amount": 7.50, "block_id": "blk-1", "status": "unclaimed", "created_at": time.Now().UTC().Format(time.RFC3339)},
		}

		r.Get("/block/countdown", func(w http.ResponseWriter, r *http.Request) {
			// simulate a remaining countdown of 120 seconds
			remaining := 120.0
			json.NewEncoder(w).Encode(map[string]interface{}{"block": currentBlock, "remaining_sec": remaining, "remaining_hms": "0h2m0s"})
		})

		r.Get("/block/current", func(w http.ResponseWriter, r *http.Request) {
			if currentBlock == nil {
				http.Error(w, "no active mint block", http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(currentBlock)
		})

		r.Get("/block/rewards", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			rewards := userBlockRewards[uid]
			// compute summary
			var sum float64
			for _, rr := range rewards {
				if v, ok := rr["amount"].(float64); ok {
					sum += v
				}
			}
			summary := map[string]interface{}{"unclaimed_total": fmt.Sprintf("%.2f", sum)}
			json.NewEncoder(w).Encode(map[string]interface{}{"rewards": rewards, "summary": summary})
		})

		r.Get("/block/rewards/history", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			// simple pagination
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
			all := userBlockRewards[uid]
			total := len(all)
			end := offset + limit
			if end > total {
				end = total
			}
			if offset > total {
				offset = total
			}
			page := all[offset:end]
			meta := map[string]interface{}{"limit": limit, "offset": offset, "total": total}
			json.NewEncoder(w).Encode(map[string]interface{}{"records": page, "meta": meta})
		})

		r.Post("/block/rewards/claim", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			var req struct {
				RewardIDs []string `json:"reward_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				web.Error(w, http.StatusBadRequest, "invalid request body")
				return
			}
			if len(req.RewardIDs) == 0 {
				web.Error(w, http.StatusBadRequest, "reward_ids cannot be empty")
				return
			}
			rewards := userBlockRewards[uid]
			var claimed float64
			// remove claimed ids from slice
			remaining := []map[string]interface{}{}
			for _, rwd := range rewards {
				idStr, _ := rwd["id"].(string)
				found := false
				for _, rid := range req.RewardIDs {
					if rid == idStr {
						if v, ok := rwd["amount"].(float64); ok {
							claimed += v
						}
						found = true
						break
					}
				}
				if !found {
					remaining = append(remaining, rwd)
				}
			}
			userBlockRewards[uid] = remaining
			json.NewEncoder(w).Encode(map[string]interface{}{"claimed_amount": fmt.Sprintf("%.2f", claimed), "claimed_count": len(req.RewardIDs)})
		})

		r.Post("/block/rewards/claim-all", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			rewards := userBlockRewards[uid]
			var claimed float64
			for _, rr := range rewards {
				if v, ok := rr["amount"].(float64); ok {
					claimed += v
				}
			}
			// clear
			userBlockRewards[uid] = []map[string]interface{}{}
			json.NewEncoder(w).Encode(map[string]interface{}{"claimed_amount": fmt.Sprintf("%.2f", claimed), "message": "all rewards claimed successfully"})
		})

		// Block network stats (dev stub)
		r.Get("/block/stats", func(w http.ResponseWriter, r *http.Request) {
			stats := map[string]interface{}{
				"total_blocks":              10,
				"total_rewards_distributed": 5000.0,
				"average_rewards_per_block": 500.0,
				"active_mint_block_present": currentBlock != nil,
			}
			json.NewEncoder(w).Encode(stats)
		})

		// --- Governance stub endpoints (public + authenticated) ---
		// seed multiple proposals for testing
		proposals := []map[string]interface{}{
			{"id": "11111111-1111-1111-1111-111111111111", "title": "Test Proposal", "description": "A sample proposal.", "status": "active", "proposer_id": testUID, "voting_end": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)},
			{"id": "22222222-2222-2222-2222-222222222222", "title": "Increase staking rewards", "description": "Proposal to increase staking rewards by 2% for 3 months.", "status": "pending", "proposer_id": testUID, "voting_end": "2026-03-01T00:00:00Z"},
			{"id": "33333333-3333-3333-3333-333333333333", "title": "Reduce transfer fees", "description": "Proposal to lower transfer fees by 10%.", "status": "finished", "proposer_id": "user-xyz", "voting_end": time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339), "result": "passed"},
			{"id": "44444444-4444-4444-4444-444444444444", "title": "Add new token listing", "description": "Proposal to list TOKENX on the platform.", "status": "finished", "proposer_id": "user-abc", "voting_end": time.Now().Add(-72 * time.Hour).UTC().Format(time.RFC3339), "result": "rejected"},
		}

		proposalVotes := make(map[string][]map[string]interface{})
		// seed votes across proposals
		proposalVotes["11111111-1111-1111-1111-111111111111"] = []map[string]interface{}{
			{"id": uuid.New().String(), "user_id": testUID, "choice": "for", "created_at": time.Now().UTC().Format(time.RFC3339)},
		}
		proposalVotes["22222222-2222-2222-2222-222222222222"] = []map[string]interface{}{
			// no votes yet for pending proposal
		}
		proposalVotes["33333333-3333-3333-3333-333333333333"] = []map[string]interface{}{
			{"id": uuid.New().String(), "user_id": "voter-1", "choice": "for", "created_at": time.Now().Add(-47 * time.Hour).UTC().Format(time.RFC3339)},
			{"id": uuid.New().String(), "user_id": "voter-2", "choice": "against", "created_at": time.Now().Add(-46 * time.Hour).UTC().Format(time.RFC3339)},
		}
		proposalVotes["44444444-4444-4444-4444-444444444444"] = []map[string]interface{}{
			{"id": uuid.New().String(), "user_id": "voter-3", "choice": "against", "created_at": time.Now().Add(-71 * time.Hour).UTC().Format(time.RFC3339)},
		}

		r.Get("/governance/proposals", func(w http.ResponseWriter, r *http.Request) {
			status := r.URL.Query().Get("status")
			if status == "active" {
				act := []map[string]interface{}{}
				for _, p := range proposals {
					if p["status"] == "active" {
						act = append(act, p)
					}
				}
				json.NewEncoder(w).Encode(act)
				return
			}
			json.NewEncoder(w).Encode(proposals)
		})

		r.Get("/governance/proposals/{id}", func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			for _, p := range proposals {
				if p["id"] == id {
					json.NewEncoder(w).Encode(p)
					return
				}
			}
			http.Error(w, "proposal not found", http.StatusNotFound)
		})

		r.Get("/governance/proposals/{id}/results", func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			votes := proposalVotes[id]
			tally := map[string]int{"for": 0, "against": 0, "abstain": 0}
			for _, v := range votes {
				if c, ok := v["choice"].(string); ok {
					tally[c]++
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"tally": tally, "total": len(votes)})
		})

		r.Get("/governance/proposals/{id}/votes", func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			json.NewEncoder(w).Encode(proposalVotes[id])
		})

		r.Get("/governance/stats", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{"total_proposals": len(proposals), "active_proposals": 1})
		})

		// Protected governance endpoints
		r.Post("/governance/proposals", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				web.Error(w, http.StatusBadRequest, "invalid request")
				return
			}
			id := uuid.New().String()
			p := map[string]interface{}{"id": id, "title": req["title"], "description": req["description"], "status": "pending", "proposer_id": u["id"], "voting_end": req["voting_end"], "created_at": time.Now().UTC().Format(time.RFC3339)}
			proposals = append(proposals, p)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(p)
		})

		r.Post("/governance/proposals/{id}/vote", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			id := chi.URLParam(r, "id")
			var req struct {
				VoteChoice string `json:"vote_choice"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VoteChoice == "" {
				web.Error(w, http.StatusBadRequest, "vote_choice required")
				return
			}
			v := map[string]interface{}{"id": uuid.New().String(), "user_id": u["id"], "choice": req.VoteChoice, "created_at": time.Now().UTC().Format(time.RFC3339)}
			proposalVotes[id] = append(proposalVotes[id], v)
			json.NewEncoder(w).Encode(v)
		})

		r.Get("/governance/user/votes", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			list := []map[string]interface{}{}
			for _, vs := range proposalVotes {
				for _, v := range vs {
					if v["user_id"] == uid {
						list = append(list, v)
					}
				}
			}
			json.NewEncoder(w).Encode(list)
		})

		// helper to create a purchase entry
		createPurchase := func(userID string, tokenSymbol string, amount float64, totalValue float64) map[string]interface{} {
			id := uuid.New().String()
			expiry := time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339)
			p := map[string]interface{}{
				"id":           id,
				"user_id":      userID,
				"token_symbol": tokenSymbol,
				"amount":       amount,
				"price_usd":    fmt.Sprintf("%.2f", totalValue/amount),
				"total_value":  fmt.Sprintf("%.2f", totalValue),
				"status":       "pending",
				"expiry_date":  expiry,
			}
			purchases[id] = p
			return p
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

		// --- Badges stub endpoints (public + authenticated) ---
		r.Get("/badges", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(badgesList)
		})

		r.Get("/badges/{id}", func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			for _, b := range badgesList {
				if b["id"] == id {
					json.NewEncoder(w).Encode(b)
					return
				}
			}
			http.Error(w, "badge not found", http.StatusNotFound)
		})

		r.Get("/badges/stats/network", func(w http.ResponseWriter, r *http.Request) {
			stats := map[string]interface{}{"total_awarded": 42, "unique_holders": 10}
			json.NewEncoder(w).Encode(stats)
		})

		r.Get("/badges/user", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			json.NewEncoder(w).Encode(userBadges[uid])
		})

		r.Get("/badges/user/active", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			active := []map[string]interface{}{}
			for _, ub := range userBadges[uid] {
				if ub["status"] == "active" {
					active = append(active, ub)
				}
			}
			json.NewEncoder(w).Encode(active)
		})

		r.Post("/badges/purchase", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			var req struct {
				BadgeID string `json:"badge_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BadgeID == "" {
				web.Error(w, http.StatusBadRequest, "badge_id required")
				return
			}
			// ensure badge exists
			found := false
			for _, b := range badgesList {
				if b["id"] == req.BadgeID {
					found = true
					break
				}
			}
			if !found {
				web.Error(w, http.StatusBadRequest, "badge not found")
				return
			}
			uid := u["id"].(string)
			ub := map[string]interface{}{"id": uuid.New().String(), "user_id": uid, "badge_id": req.BadgeID, "purchased_at": time.Now().UTC().Format(time.RFC3339), "status": "active"}
			userBadges[uid] = append(userBadges[uid], ub)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(ub)
		})

		r.Get("/badges/records", func(w http.ResponseWriter, r *http.Request) {
			// aggregate all user badges as records
			recs := []map[string]interface{}{}
			for _, list := range userBadges {
				for _, ub := range list {
					recs = append(recs, ub)
				}
			}
			// support pagination params
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
			total := len(recs)
			end := offset + limit
			if end > total {
				end = total
			}
			if offset > total {
				offset = total
			}
			page := recs[offset:end]
			meta := map[string]interface{}{"limit": limit, "offset": offset, "total": total}
			json.NewEncoder(w).Encode(map[string]interface{}{"records": page, "meta": meta})
		})

		// --- Combat stub endpoints (authenticated/public) ---

		// --- LP (liquidity) stub endpoints (public + authenticated) ---
		lpPools := []map[string]interface{}{
			{"id": 1, "token_a": "CGL", "token_b": "ETH", "total_liquidity_usd": 12345.67},
		}
		userLP := make(map[string][]map[string]interface{})

		r.Get("/lp/pools", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(lpPools)
		})

		r.Get("/lp/pools/{id}", func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			for _, p := range lpPools {
				if fmt.Sprintf("%v", p["id"]) == id {
					json.NewEncoder(w).Encode(p)
					return
				}
			}
			http.Error(w, "pool not found", http.StatusNotFound)
		})

		r.Get("/lp/stats", func(w http.ResponseWriter, r *http.Request) {
			total := 0
			var totalUSD float64
			for _, p := range lpPools {
				total++
				if v, ok := p["total_liquidity_usd"].(float64); ok {
					totalUSD += v
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"total_pools": total, "total_liquidity_usd": totalUSD})
		})

		r.Get("/lp/user/balances", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			json.NewEncoder(w).Encode(userLP[uid])
		})

		r.Post("/lp/pools/{id}/add", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			var req struct {
				TokenA  string  `json:"token_a"`
				AmountA float64 `json:"amount_a"`
				TokenB  string  `json:"token_b"`
				AmountB float64 `json:"amount_b"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				web.Error(w, http.StatusBadRequest, "invalid request")
				return
			}
			// simplistic LP token calc: 1 LP token per 10 USD added
			lpTokens := (req.AmountA*0.42 + req.AmountB*1800.0) / 10.0
			entry := map[string]interface{}{"pool_id": chi.URLParam(r, "id"), "lp_tokens": lpTokens, "token_a": req.TokenA, "token_b": req.TokenB, "amount_a": req.AmountA, "amount_b": req.AmountB, "created_at": time.Now().UTC().Format(time.RFC3339)}
			uid := u["id"].(string)
			userLP[uid] = append(userLP[uid], entry)
			json.NewEncoder(w).Encode(map[string]interface{}{"message": "liquidity added", "lp_tokens_received": fmt.Sprintf("%.2f", lpTokens), "entry": entry})
		})

		r.Post("/lp/pools/{id}/remove", func(w http.ResponseWriter, r *http.Request) {
			_, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			var req struct {
				LPTokenAmount float64 `json:"lp_token_amount"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LPTokenAmount <= 0 {
				web.Error(w, http.StatusBadRequest, "lp_token_amount required")
				return
			}
			// naive proportional withdrawal: return amounts based on fixed ratios
			amountA := req.LPTokenAmount * 1.0
			amountB := req.LPTokenAmount * 0.0005
			json.NewEncoder(w).Encode(map[string]interface{}{"message": "liquidity removed", "amounts": map[string]float64{"CGL": amountA, "ETH": amountB}, "lp_tokens_burned": req.LPTokenAmount})
		})

		r.Get("/combat/network", func(w http.ResponseWriter, r *http.Request) {
			// simple static network power
			json.NewEncoder(w).Encode(map[string]interface{}{"network_combat_power": 98765.43})
		})

		r.Get("/combat/personal", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			// return a fabricated combat power record for dev
			resp := map[string]interface{}{
				"user_id":        u["id"],
				"personal_power": "123.45",
				"network_power":  "98765.43",
				"lp_weight":      "10.00",
				"burn_power":     "5.00",
				"updated_at":     time.Now().UTC().Format(time.RFC3339),
			}
			json.NewEncoder(w).Encode(resp)
		})

		r.Get("/combat/history", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			days := 7
			if d := r.URL.Query().Get("days"); d != "" {
				if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 90 {
					days = n
				}
			}
			// fabricate daily snapshots
			history := []map[string]interface{}{}
			for i := days - 1; i >= 0; i-- {
				date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
				history = append(history, map[string]interface{}{"date": date, "personal_power": "100.0"})
			}
			_ = u
			json.NewEncoder(w).Encode(history)
		})

		r.Post("/combat/refresh", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			_ = u
			json.NewEncoder(w).Encode(map[string]string{"message": "combat power updated successfully"})
		})

		// --- Swaps stub endpoints (public + authenticated) ---
		swapsStore := make(map[string]map[string]interface{})

		r.Get("/swaps/rate", func(w http.ResponseWriter, r *http.Request) {
			from := strings.ToUpper(r.URL.Query().Get("from"))
			to := strings.ToUpper(r.URL.Query().Get("to"))
			if from == "" || to == "" {
				web.Error(w, http.StatusBadRequest, "from and to are required")
				return
			}
			// simple deterministic rate for dev
			rate := 0.42
			if from == "CAN" && to == "USDT" {
				rate = 2.38
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"from": from, "to": to, "rate": fmt.Sprintf("%.6f", rate)})
		})

		r.Post("/swaps/execute", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				web.Error(w, http.StatusBadRequest, "invalid request body")
				return
			}
			fromToken, _ := body["from_token"].(string)
			toToken, _ := body["to_token"].(string)
			txHash, _ := body["tx_hash"].(string)
			if fromToken == "" || toToken == "" {
				web.Error(w, http.StatusBadRequest, "from_token and to_token required")
				return
			}
			// parse amount which may be number or string
			var amount float64
			switch v := body["amount"].(type) {
			case float64:
				amount = v
			case string:
				if a, err := strconv.ParseFloat(v, 64); err == nil {
					amount = a
				} else {
					web.Error(w, http.StatusBadRequest, "invalid amount format")
					return
				}
			case json.Number:
				if a, err := v.Float64(); err == nil {
					amount = a
				} else {
					web.Error(w, http.StatusBadRequest, "invalid amount format")
					return
				}
			default:
				web.Error(w, http.StatusBadRequest, "amount is required and must be a number")
				return
			}
			// allow zero amounts for dev testing; reject negative values
			if amount < 0 {
				web.Error(w, http.StatusBadRequest, "amount must be non-negative")
				return
			}
			if txHash == "" {
				web.Error(w, http.StatusBadRequest, "tx_hash required")
				return
			}
			// compute received using static rate
			rate := 0.42
			received := amount * rate
			id := uuid.New().String()
			s := map[string]interface{}{"id": id, "user_id": u["id"], "from_token": fromToken, "to_token": toToken, "amount": fmt.Sprintf("%.6f", amount), "received": fmt.Sprintf("%.6f", received), "tx_hash": txHash, "status": "completed", "created_at": time.Now().UTC().Format(time.RFC3339)}
			swapsStore[id] = s
			json.NewEncoder(w).Encode(s)
		})

		r.Get("/swaps", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			list := []map[string]interface{}{}
			for _, s := range swapsStore {
				if s["user_id"] == u["id"] {
					list = append(list, s)
				}
			}
			json.NewEncoder(w).Encode(list)
		})

		r.Get("/swaps/{id}", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			id := chi.URLParam(r, "id")
			s, ok := swapsStore[id]
			if !ok || s["user_id"] != u["id"] {
				web.Error(w, http.StatusNotFound, "swap not found")
				return
			}
			json.NewEncoder(w).Encode(s)
		})

		r.Get("/swaps/stats", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			total := 0
			var volume float64
			for _, s := range swapsStore {
				if s["user_id"] == u["id"] {
					total++
					if aStr, ok := s["amount"].(string); ok {
						if f, err := strconv.ParseFloat(aStr, 64); err == nil {
							volume += f
						}
					}
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"total_swaps": total, "total_volume": fmt.Sprintf("%.6f", volume)})
		})

		// --- Mining stub endpoints (authenticated) ---
		r.Get("/mining/machine", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			uid := u["id"].(string)
			machine := map[string]interface{}{
				"user_id":           uid,
				"level":             3,
				"hash_rate":         "123.45",
				"efficiency":        "1.23",
				"next_upgrade_cost": map[string]interface{}{"token": "CGL", "amount": 150.0},
				"updated_at":        time.Now().UTC().Format(time.RFC3339),
			}
			json.NewEncoder(w).Encode(machine)
		})

		r.Post("/mining/upgrade", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			_ = u
			// Simulate an upgrade: return bumped level and hash rate
			machine := map[string]interface{}{"message": "machine upgraded", "machine": map[string]interface{}{"level": 4, "hash_rate": "150.00", "updated_at": time.Now().UTC().Format(time.RFC3339)}}
			json.NewEncoder(w).Encode(machine)
		})

		r.Get("/mining/earnings/today", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			_ = u
			// Return a projected earnings value for dev
			json.NewEncoder(w).Encode(map[string]interface{}{"projected": true, "earnings": map[string]interface{}{"amount": "2.50", "token": "CGL"}})
		})

		r.Get("/mining/earnings/history", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			_ = u
			days := 30
			if d := r.URL.Query().Get("days"); d != "" {
				if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 90 {
					days = n
				}
			}
			limit := 20
			offset := 0
			if l := r.URL.Query().Get("limit"); l != "" {
				if n, err := strconv.Atoi(l); err == nil {
					limit = n
				}
			}
			if o := r.URL.Query().Get("offset"); o != "" {
				if n, err := strconv.Atoi(o); err == nil {
					offset = n
				}
			}
			history := []map[string]interface{}{}
			for i := days - 1; i >= 0; i-- {
				date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
				history = append(history, map[string]interface{}{"date": date, "amount": "1.00", "token": "CGL"})
			}
			total := len(history)
			end := offset + limit
			if end > total {
				end = total
			}
			if offset > total {
				offset = total
			}
			page := history[offset:end]
			meta := map[string]interface{}{"limit": limit, "offset": offset, "total": total}
			json.NewEncoder(w).Encode(map[string]interface{}{"records": page, "meta": meta})
		})

		r.Post("/mining/earnings/accrue", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			_ = u
			json.NewEncoder(w).Encode(map[string]interface{}{"message": "Daily earnings accrued successfully", "earnings": map[string]interface{}{"amount": "2.50", "token": "CGL"}})
		})

		r.Get("/mining/stats", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			_ = u
			json.NewEncoder(w).Encode(map[string]interface{}{"total_mined": "12345.67", "today": "2.50", "machine_level": 3, "active_miners": 10})
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

		// --- Purchases stub endpoints (authenticated) ---
		r.Post("/purchases/subscribe", func(w http.ResponseWriter, r *http.Request) {
			user, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			var req struct {
				TokenSymbol  string  `json:"token_symbol"`
				Amount       float64 `json:"amount"`
				PaymentToken string  `json:"payment_token"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				web.Error(w, http.StatusBadRequest, "invalid request body")
				return
			}
			if req.TokenSymbol == "" || req.Amount <= 0 || req.PaymentToken == "" {
				web.Error(w, http.StatusBadRequest, "token_symbol, positive amount and payment_token required")
				return
			}

			// Simplified price lookup
			price := 1.23
			total := price * req.Amount
			p := createPurchase(user["id"].(string), req.TokenSymbol, req.Amount, total)

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"purchase":    p,
				"total_value": fmt.Sprintf("%.2f", total),
				"status":      "pending",
				"expiry":      p["expiry_date"],
			})
		})

		r.Get("/purchases", func(w http.ResponseWriter, r *http.Request) {
			user, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			limitStr := r.URL.Query().Get("limit")
			offsetStr := r.URL.Query().Get("offset")
			_ = limitStr
			_ = offsetStr
			// return all purchases for the user
			list := []map[string]interface{}{}
			for _, p := range purchases {
				if p["user_id"] == user["id"] {
					list = append(list, p)
				}
			}
			json.NewEncoder(w).Encode(list)
		})

		r.Get("/purchases/{id}", func(w http.ResponseWriter, r *http.Request) {
			user, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			id := chi.URLParam(r, "id")
			p, ok := purchases[id]
			if !ok || p["user_id"] != user["id"] {
				web.Error(w, http.StatusNotFound, "purchase not found")
				return
			}
			json.NewEncoder(w).Encode(p)
		})

		// --- Withdrawals stub endpoints (authenticated) ---
		withdrawalsStore := make(map[string]map[string]interface{})

		r.Post("/withdrawals", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				web.Error(w, http.StatusBadRequest, "invalid request body")
				return
			}
			tokenSym, _ := body["token_symbol"].(string)
			if tokenSym == "" {
				web.Error(w, http.StatusBadRequest, "token_symbol required")
				return
			}
			// parse amount which may be number or string
			var amount float64
			switch v := body["amount"].(type) {
			case float64:
				amount = v
			case string:
				if a, err := strconv.ParseFloat(v, 64); err == nil {
					amount = a
				} else {
					web.Error(w, http.StatusBadRequest, "invalid amount format")
					return
				}
			case json.Number:
				if a, err := v.Float64(); err == nil {
					amount = a
				} else {
					web.Error(w, http.StatusBadRequest, "invalid amount format")
					return
				}
			default:
				web.Error(w, http.StatusBadRequest, "amount is required and must be a number")
				return
			}
			if amount <= 0 {
				web.Error(w, http.StatusBadRequest, "amount must be positive")
				return
			}
			id := uuid.New().String()
			rec := map[string]interface{}{"id": id, "user_id": u["id"], "token_symbol": strings.ToUpper(tokenSym), "amount": fmt.Sprintf("%.6f", amount), "status": "pending", "created_at": time.Now().UTC().Format(time.RFC3339)}
			withdrawalsStore[id] = rec
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(rec)
		})

		r.Get("/withdrawals", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			list := []map[string]interface{}{}
			for _, wd := range withdrawalsStore {
				if wd["user_id"] == u["id"] {
					list = append(list, wd)
				}
			}
			json.NewEncoder(w).Encode(list)
		})

		r.Get("/withdrawals/{id}", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			id := chi.URLParam(r, "id")
			wd, ok := withdrawalsStore[id]
			if !ok || wd["user_id"] != u["id"] {
				web.Error(w, http.StatusNotFound, "withdrawal not found")
				return
			}
			json.NewEncoder(w).Encode(wd)
		})

		r.Post("/withdrawals/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			id := chi.URLParam(r, "id")
			wd, ok := withdrawalsStore[id]
			if !ok || wd["user_id"] != u["id"] {
				web.Error(w, http.StatusNotFound, "withdrawal not found")
				return
			}
			wd["status"] = "cancelled"
			json.NewEncoder(w).Encode(map[string]string{"message": "withdrawal cancelled successfully"})
		})

		r.Get("/withdrawals/limits", func(w http.ResponseWriter, r *http.Request) {
			u, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			_ = u
			limits := map[string]interface{}{"daily_limit": 1000.0, "remaining": 500.0, "currency": "USDT"}
			json.NewEncoder(w).Encode(limits)
		})

		r.Post("/purchases/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
			user, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			id := chi.URLParam(r, "id")
			p, ok := purchases[id]
			if !ok || p["user_id"] != user["id"] {
				web.Error(w, http.StatusNotFound, "purchase not found")
				return
			}
			var req struct {
				TxHash string `json:"tx_hash"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TxHash == "" {
				web.Error(w, http.StatusBadRequest, "tx_hash required")
				return
			}
			p["status"] = "completed"
			p["tx_hash"] = req.TxHash
			json.NewEncoder(w).Encode(map[string]interface{}{"message": "Purchase completed successfully", "purchase": p})
		})

		r.Post("/purchases/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
			user, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			id := chi.URLParam(r, "id")
			p, ok := purchases[id]
			if !ok || p["user_id"] != user["id"] {
				web.Error(w, http.StatusNotFound, "purchase not found")
				return
			}
			p["status"] = "cancelled"
			json.NewEncoder(w).Encode(map[string]string{"message": "Purchase cancelled successfully"})
		})

		r.Get("/assets/stats/network", func(w http.ResponseWriter, r *http.Request) {
			stats := map[string]interface{}{"tvl_usd": 1234567.0, "unique_holders": 54321}
			json.NewEncoder(w).Encode(stats)
		})

		// --- Burn endpoint for local testing (requires Bearer token) ---
		r.Post("/burns", func(w http.ResponseWriter, r *http.Request) {
			user, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			_ = user

			var req struct {
				TokenSymbol string  `json:"token_symbol"`
				Amount      float64 `json:"amount"`
				TxHash      string  `json:"tx_hash"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				web.Error(w, http.StatusBadRequest, "invalid request")
				return
			}

			// basic validation
			if req.TokenSymbol == "" || req.Amount <= 0 {
				web.Error(w, http.StatusBadRequest, "token_symbol and positive amount required")
				return
			}

			// ensure token exists in tokensList
			sym := strings.ToUpper(req.TokenSymbol)
			found := false
			for _, t := range tokensList {
				if strings.EqualFold(t["symbol"].(string), sym) {
					found = true
					break
				}
			}
			if !found {
				web.Error(w, http.StatusBadRequest, "token not found")
				return
			}

			// In-memory response: accept the burn and return success
			json.NewEncoder(w).Encode(map[string]string{"status": "burned"})
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

		// --- Dashboard stub endpoint (auth required) ---
		r.Get("/dashboard", func(w http.ResponseWriter, r *http.Request) {
			_, err := parseUser(r)
			if err != nil {
				web.Unauthorized(w, "user not authenticated")
				return
			}
			// build a lightweight dashboard response using in-memory data
			cp := 123.45
			networkCP := 98765.43
			balances := []map[string]interface{}{
				{"symbol": "CGL", "balance": 1000, "usd": 420.0},
				{"symbol": "ETH", "balance": 0.5, "usd": 900.0},
			}
			miningToday := 12.34
			blockCountdown := 120.0
			badges := []map[string]interface{}{{"id": "b1", "name": "Early Miner"}}
			node := map[string]interface{}{"level": "silver", "power": 42}

			response := map[string]interface{}{
				"combat_power":          cp,
				"network_combat_power":  networkCP,
				"balances":              balances,
				"mining_earnings_today": miningToday,
				"block_countdown_sec":   blockCountdown,
				"badges":                badges,
				"node_level":            node,
			}
			json.NewEncoder(w).Encode(response)
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
