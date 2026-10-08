// Independent Node verification of the public SMIP/0.1 interoperability vector.
import { readFile } from 'node:fs/promises'
import { createHash, createPublicKey, verify } from 'node:crypto'
import assert from 'node:assert/strict'
const vector = JSON.parse(await readFile(new URL('../internal/smip/testdata/file-v0.1.json', import.meta.url), 'utf8'))
const { envelope: e, signature } = vector.packet
const r = vector.receipt
const decode = value => Buffer.from(value, 'base64url')
const publicKey = value => createPublicKey({ key: Buffer.concat([Buffer.from('302a300506032b6570032100', 'hex'), decode(value)]), format: 'der', type: 'spki' })
const hash = bytes => createHash('sha256').update(bytes).digest('hex')
assert(Number.isSafeInteger(e.created) && Number.isSafeInteger(e.expires) && Number.isSafeInteger(r.accepted))
const envelope = Buffer.from('SMIP-ENVELOPE\n' + JSON.stringify([e.version,e.id,e.from,e.to,e.sender,e.recipient,e.stream,e.kind,e.created,e.expires,e.keyId,e.name,e.payload]))
const receipt = Buffer.from('SMIP-RECEIPT\n' + JSON.stringify([r.version,r.id,r.from,r.to,r.digest,r.accepted,r.keyId]))
assert.deepEqual(envelope, decode(vector.envelopeSigningBytes))
assert.deepEqual(receipt, decode(vector.receiptSigningBytes))
assert.equal(hash(decode(vector.senderPublic)), e.keyId)
assert.equal(hash(decode(vector.receiverPublic)), r.keyId)
assert.equal(hash(envelope), r.digest)
assert(verify(null, envelope, publicKey(vector.senderPublic), decode(signature)))
assert(verify(null, receipt, publicKey(vector.receiverPublic), decode(r.signature)))
assert.deepEqual(decode(e.payload), Buffer.from([0,255,7,3,0,9]))
assert.equal(decode(e.name).toString('utf8'), 'résumé.bin')
assert(!verify(null, Buffer.concat([envelope,Buffer.from('tampered')]), publicKey(vector.senderPublic), decode(signature)))
console.log('SMIP/0.1 public file vector: canonical bytes, key IDs, digest and both Ed25519 signatures verified independently in Node.')
