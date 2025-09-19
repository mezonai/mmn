#!/bin/bash

copy_with_feedback() {
    local src="$1"
    local dest="$2"

    # Check if source file exists
    if [ ! -f "$src" ]; then
        echo "Error: '$src' does not exist"
        return 1
    fi

    # Check if destination file already exists
    if [ -f "$dest" ]; then
        echo "Skipped: $dest (already exists)"
        return 0
    else
        # Copy the file
        if cp "$src" "$dest" 2>/dev/null; then
            echo "Created: $dest"
            return 0
        else
            echo "Error: Failed to copy $src to $dest"
            return 1
        fi
    fi
}

copy_with_feedback ".env.example" ".env"
copy_with_feedback "./monitoring/config/prometheus/targets/nodes.example.yml" "./monitoring/config/prometheus/targets/nodes.yml"