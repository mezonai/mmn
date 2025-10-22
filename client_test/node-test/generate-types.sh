#!/bin/bash

# Generate TypeScript interfaces from proto files
# This script uses the Windows batch file to handle protoc plugin compatibility

set -e

PROTO_DIR="./proto"
GENERATED_DIR="./generated"

# Create generated directory if it doesn't exist
mkdir -p "${GENERATED_DIR}"

echo "🔍 Found proto files:"
ls -la "${PROTO_DIR}"/*.proto

echo ""
echo "🚀 Generating TypeScript interfaces..."

# Use the Windows batch file to handle protoc plugin compatibility
echo "📝 Using Windows batch file for protoc plugin compatibility..."
./generate-types.bat

echo ""
echo "🎉 TypeScript interface generation completed!"
echo "📁 Generated files are in: ${GENERATED_DIR}"
echo ""
echo "📋 Generated files:"
ls -la "${GENERATED_DIR}"/
