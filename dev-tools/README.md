# dev-tools

Small helpers for local development.

## sign.js

Signs a nonce using a local Ethereum private key (ethers.js).

Install dependencies in the repo root:

```bash
npm install
```

Usage:

```bash
node dev-tools/sign.js <privateKey> <nonce>
# Example:
node dev-tools/sign.js 0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 3f6d...
```
