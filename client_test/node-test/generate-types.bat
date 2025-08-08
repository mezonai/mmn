@echo off
setlocal enabledelayedexpansion

echo 🔍 Found proto files:
dir proto\*.proto

echo.
echo 🚀 Generating TypeScript interfaces...

set PROTO_DIR=proto
set GENERATED_DIR=generated

if not exist "%GENERATED_DIR%" mkdir "%GENERATED_DIR%"

for %%f in (%PROTO_DIR%\*.proto) do (
    set "filename=%%~nf"
    echo 📝 Generating interfaces for !filename!.proto...
    
    npx protoc ^
        --proto_path="%PROTO_DIR%" ^
        --plugin=protoc-gen-ts=%CD%\node_modules\.bin\protoc-gen-ts.cmd ^
        --ts_out="%GENERATED_DIR%" ^
        "%%~nxf"
    
    if !errorlevel! equ 0 (
        echo ✅ Generated TypeScript interfaces for !filename!.proto
    ) else (
        echo ❌ Failed to generate TypeScript interfaces for !filename!.proto
    )
)

echo.
echo 🎉 TypeScript interface generation completed!
echo 📁 Generated files are in: %GENERATED_DIR%
echo.
echo 📋 Generated files:
dir "%GENERATED_DIR%"

endlocal 