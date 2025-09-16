#!/bin/bash
set -e

HTPASSWD_FILE=./htpasswd

echo "Basic auth credentials for new loki client (promtail):"
read -p "> Enter username: " USER
read -s -p "> Enter password: " PASS
echo

echo "Generating htpasswd file for user '$USER'..."

# Use lightweight httpd:alpine to generate hashed password file then remove container after running
# Use flag -B to hash password with bcrypt algo
docker run --rm \
    httpd:2.4-alpine \
    htpasswd -Bbn "$USER" "$PASS" >> "$HTPASSWD_FILE"