// Copyright (c) 2015-2017 The Decred developers 
// Copyright (c) 2018-2020 The Hc developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package edwards

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"math/rand"
	"testing"
)

type signerHex struct {
	privkey          string
	privateNonce     string
	pubKeySumLocal   string
	partialSignature string
}

type ThresholdTestVectorHex struct {
	msg               string
	signersHex        []signerHex
	combinedSignature string
}

type signer struct {
	privkey          []byte
	pubkey           *PublicKey
	privateNonce     []byte
	publicNonce      *PublicKey
	pubKeySumLocal   *PublicKey
	partialSignature []byte
}

type ThresholdTestVector struct {
	msg               []byte
	signers           []signer
	combinedSignature []byte
}

func TestSchnorrThreshold(t *testing.T) {
	tRand := rand.New(rand.NewSource(543212345))
	maxSignatories := 10
	numTests := 5
	numSignatories := maxSignatories * numTests

	curve := new(TwistedEdwardsCurve)
	curve.InitParam25519()

	msg, _ := hex.DecodeString(
		"d04b98f48e8f8bcc15c6ae5ac050801cd6dcfd428fb5f9e65c4e16e7807340fa")
	privkeys := randPrivScalarKeyList(curve, numSignatories)

	for i := 0; i < numTests; i++ {
		numKeysForTest := tRand.Intn(maxSignatories-2) + 2
		keyIndex := i * maxSignatories
		keysToUse := make([]*PrivateKey, numKeysForTest, numKeysForTest)
		for j := 0; j < numKeysForTest; j++ {
			keysToUse[j] = privkeys[j+keyIndex]
		}

		pubKeysToUse := make([]*PublicKey, numKeysForTest,
			numKeysForTest)
		for j := 0; j < numKeysForTest; j++ {
			_, pubkey, _ := PrivKeyFromScalar(curve,
				keysToUse[j].Serialize())
			pubKeysToUse[j] = pubkey
		}

		// Combine pubkeys.
		allPubkeys := make([]*PublicKey, numKeysForTest)
		copy(allPubkeys, pubKeysToUse)

		allPksSum := CombinePubkeys(curve, allPubkeys)

		privNoncesToUse := make([]*PrivateKey, numKeysForTest,
			numKeysForTest)
		pubNoncesToUse := make([]*PublicKey, numKeysForTest,
			numKeysForTest)
		for j := 0; j < numKeysForTest; j++ {
			nonce := nonceRFC6979(curve, keysToUse[j].Serialize(), msg, nil,
				Sha512VersionStringRFC6979)
			nonceBig := new(big.Int).SetBytes(nonce)
			nonceBig.Mod(nonceBig, curve.N)
			nonce = copyBytes(nonceBig.Bytes())[:]
			nonce[31] &= 248

			privNonce, pubNonce, err := PrivKeyFromScalar(curve,
				nonce[:])
			cmp := privNonce != nil
			if !cmp {
				t.Fatalf("expected %v, got %v", true, cmp)
			}

			cmp = pubNonce != nil
			if !cmp {
				t.Fatalf("expected %v, got %v", true, cmp)
			}

			if err != nil {
				t.Fatalf("unexpected error %s, ", err)
			}

			privNoncesToUse[j] = privNonce
			pubNoncesToUse[j] = pubNonce
		}

		partialSignatures := make([]*Signature, numKeysForTest, numKeysForTest)

		// Partial signature generation.
		publicNonceSum := CombinePubkeys(curve, pubNoncesToUse)
		cmp := publicNonceSum != nil
		if !cmp {
			t.Fatalf("expected %v, got %v", true, cmp)
		}
		for j := range keysToUse {
			r, s, err := schnorrPartialSign(curve, msg, keysToUse[j].Serialize(),
				allPksSum.Serialize(), privNoncesToUse[j].Serialize(),
				publicNonceSum.Serialize())
			if err != nil {
				t.Fatalf("unexpected error %s, ", err)
			}

			localSig := NewSignature(r, s)
			partialSignatures[j] = localSig
		}

		// Combine signatures.
		combinedSignature, err := SchnorrCombineSigs(curve, partialSignatures)
		if err != nil {
			t.Fatalf("unexpected error %s, ", err)
		}

		// Make sure the combined signatures are the same as the
		// signatures that would be generated by simply adding
		// the private keys and private nonces.
		combinedPrivkeysD := new(big.Int).SetInt64(0)
		for _, priv := range keysToUse {
			combinedPrivkeysD = ScalarAdd(combinedPrivkeysD, priv.GetD())
			combinedPrivkeysD = combinedPrivkeysD.Mod(combinedPrivkeysD, curve.N)
		}

		combinedNonceD := new(big.Int).SetInt64(0)
		for _, priv := range privNoncesToUse {
			combinedNonceD.Add(combinedNonceD, priv.GetD())
			combinedNonceD.Mod(combinedNonceD, curve.N)
		}

		combinedPrivkey, _, err := PrivKeyFromScalar(curve,
			copyBytes(combinedPrivkeysD.Bytes())[:])
		if err != nil {
			t.Fatalf("unexpected error %s, ", err)
		}
		combinedNonce, _, err := PrivKeyFromScalar(curve,
			copyBytes(combinedNonceD.Bytes())[:])
		if err != nil {
			t.Fatalf("unexpected error %s, ", err)
		}
		cSigR, cSigS, err := SignFromScalar(curve, combinedPrivkey,
			combinedNonce.Serialize(), msg)
		sumSig := NewSignature(cSigR, cSigS)
		cmp = bytes.Equal(sumSig.Serialize(), combinedSignature.Serialize())
		if !cmp {
			t.Fatalf("expected %v, got %v", true, cmp)
		}

		if err != nil {
			t.Fatalf("unexpected error %s, ", err)
		}

		// Verify the combined signature and public keys.
		ok := Verify(allPksSum, msg, combinedSignature.GetR(),
			combinedSignature.GetS())
		if !ok {
			t.Fatalf("expected %v, got %v", true, ok)
		}

		// Corrupt some memory and make sure it breaks something.
		corruptWhat := tRand.Intn(3)
		randItem := tRand.Intn(numKeysForTest - 1)

		// Corrupt private key.
		if corruptWhat == 0 {
			privSerCorrupt := keysToUse[randItem].Serialize()
			pos := tRand.Intn(31)
			bitPos := tRand.Intn(7)
			privSerCorrupt[pos] ^= 1 << uint8(bitPos)
			keysToUse[randItem].ecPk.D.SetBytes(privSerCorrupt)
		}
		// Corrupt public key.
		if corruptWhat == 1 {
			pubXCorrupt := BigIntToEncodedBytes(pubKeysToUse[randItem].GetX())
			pos := tRand.Intn(31)
			bitPos := tRand.Intn(7)
			pubXCorrupt[pos] ^= 1 << uint8(bitPos)
			pubKeysToUse[randItem].GetX().SetBytes(pubXCorrupt[:])
		}
		// Corrupt private nonce.
		if corruptWhat == 2 {
			privSerCorrupt := privNoncesToUse[randItem].Serialize()
			pos := tRand.Intn(31)
			bitPos := tRand.Intn(7)
			privSerCorrupt[pos] ^= 1 << uint8(bitPos)
			privNoncesToUse[randItem].ecPk.D.SetBytes(privSerCorrupt)
		}
		// Corrupt public nonce.
		if corruptWhat == 3 {
			pubXCorrupt := BigIntToEncodedBytes(pubNoncesToUse[randItem].GetX())
			pos := tRand.Intn(31)
			bitPos := tRand.Intn(7)
			pubXCorrupt[pos] ^= 1 << uint8(bitPos)
			pubNoncesToUse[randItem].GetX().SetBytes(pubXCorrupt[:])
		}

		for j := range keysToUse {
			thisPubNonce := pubNoncesToUse[j]
			localPubNonces := make([]*PublicKey, numKeysForTest-1,
				numKeysForTest-1)
			itr := 0
			for _, pubNonce := range pubNoncesToUse {
				if bytes.Equal(thisPubNonce.Serialize(), pubNonce.Serialize()) {
					continue
				}
				localPubNonces[itr] = pubNonce
				itr++
			}
			publicNonceSum := CombinePubkeys(curve, localPubNonces)

			sigR, sigS, _ := schnorrPartialSign(curve, msg,
				keysToUse[j].Serialize(), allPksSum.Serialize(),
				privNoncesToUse[j].Serialize(),
				publicNonceSum.Serialize())
			localSig := NewSignature(sigR, sigS)

			partialSignatures[j] = localSig
		}

		// Combine signatures.
		combinedSignature, _ = SchnorrCombineSigs(curve, partialSignatures)

		// Nothing that makes it here should be valid.
		if allPksSum != nil && combinedSignature != nil {
			ok = Verify(allPksSum, msg, combinedSignature.GetR(),
				combinedSignature.GetS())
			if ok {
				t.Fatalf("expected %v, got %v", false, ok)
			}
		}
	}
}