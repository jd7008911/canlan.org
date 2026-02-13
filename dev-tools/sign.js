const { ethers } = require('ethers');
// Usage: node dev-tools/sign.js <privateKey> <nonce>
// Example: node dev-tools/sign.js 0xaaaaaaaa... 0x1234abcd

async function main() {
  const pk = process.argv[2];
  const nonce = process.argv[3];
  if (!pk || !nonce) {
    console.error('Usage: node dev-tools/sign.js <privateKey> <nonce>');
    process.exit(2);
  }
  const wallet = new ethers.Wallet(pk);
  const sig = await wallet.signMessage(nonce);
  console.log(sig);
}

main().catch(e => { console.error(e); process.exit(1); });
