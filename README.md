# Canglanfu API (dev notes)

This repository contains the API and development helpers.

## Dev tools

A small helper for signing nonces is available under `dev-tools`.

- Install Node dependencies from the repo root or simply for dev-tools:

```bash
npm --prefix dev-tools install
```

- Start the local dev server (Go):

```bash
go run ./cmd/dev
```

- Sign a nonce and log in (example):

```bash
# get derived wallet (example private key)
node -e "const { ethers } = require('ethers'); console.log(new ethers.Wallet('0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa').address)"
nonce=$(curl -s -X POST http://localhost:8080/api/v1/auth/nonce -H 'Content-Type: application/json' -d '{"wallet":"<derived_address>"}' | jq -r '.nonce')
sig=$(node dev-tools/sign.js 0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "$nonce")
token=$(curl -s -X POST http://localhost:8080/api/v1/auth/login -H 'Content-Type: application/json' -d '{"wallet":"<derived_address>","signature":"'$sig'","referral":""}' | jq -r '.token')
curl -s http://localhost:8080/api/v1/auth/me -H "Authorization: Bearer $token"
```

Files:
- `dev-tools/sign.js` — signs a nonce using ethers.js
- `dev-tools/package.json` — Node deps for the helper
# canlan.org
