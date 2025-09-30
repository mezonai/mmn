#!/bin/bash

copy_with_feedback() {
    local src="$1"
    local dest="$2"
    local compare="$3"  # Optional: if true, compare files and update if different

    # Check if source file exists
    if [ ! -f "$src" ]; then
        echo "Error: '$src' does not exist"
        return 1
    fi

    # Check if destination file already exists
    if [ -f "$dest" ]; then
        if [ "$compare" = "true" ]; then
            # Compare files and update if different
            if ! cmp -s "$src" "$dest"; then
                echo "Updated: $dest (content differs from $src)"
                if cp "$src" "$dest" 2>/dev/null; then
                    return 0
                else
                    echo "Error: Failed to update $dest"
                    return 1
                fi
            else
                echo "Skipped: $dest (already exists)"
                return 0
            fi
        else
            echo "Skipped: $dest (already exists)"
            return 0
        fi
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

copy_with_feedback ".env.example" ".env" "true"
copy_with_feedback "./monitoring/config/prometheus/targets/nodes.example.yml" "./monitoring/config/prometheus/targets/nodes.yml"