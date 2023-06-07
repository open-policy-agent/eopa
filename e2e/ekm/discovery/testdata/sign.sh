#!/bin/bash
# openssl genpkey -algorithm RSA -out private_key.pem -pkeyopt rsa_keygen_bits:2048
# openssl rsa -pubout -in private_key.pem -out public_key.pem

../../../../bin/eopa sign --signing-key private_key.pem --bundle build/
cp .signatures.json build/.

../../../../bin/eopa build --bundle --signing-key private_key.pem --verification-key public_key.pem build/ -o discovery.tar.gz
rm .signatures.json
