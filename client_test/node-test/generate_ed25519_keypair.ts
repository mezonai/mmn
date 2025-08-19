import nacl from "tweetnacl";

// Generate a random Ed25519 keypair
const keyPair = nacl.sign.keyPair();

// The 32-byte seed (private key for Ed25519)
const seedHex = Buffer.from(keyPair.secretKey).slice(0, 32).toString("hex");
// The 32-byte public key
const publicKeyHex = Buffer.from(keyPair.publicKey).toString("hex");

// For Node.js crypto.createPrivateKey, you need PKCS8 DER encoding
const pkcs8Prefix = Buffer.from("302e020100300506032b657004220420", "hex");
const pkcs8Der = Buffer.concat([pkcs8Prefix, Buffer.from(seedHex, "hex")]);
const pkcs8DerHex = pkcs8Der.toString("hex");

console.log("Ed25519 key pair for hardcoding:");
console.log("Raw 32-byte private key (seed):", seedHex);
console.log("Raw 32-byte public key:", publicKeyHex);
console.log("PKCS8 DER private key hex (for Node.js crypto):", pkcs8DerHex);

console.log("\n--- For test.ts ---");
console.log(`const faucetPrivateKeyHex = "${pkcs8DerHex}";`);
console.log(`const faucetPublicKeyHex = "${publicKeyHex}";`);

console.log("\n--- For genesis config ---");
console.log(`faucet_address: "${publicKeyHex}"`); 