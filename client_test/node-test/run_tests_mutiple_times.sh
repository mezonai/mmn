#!/bin/bash

# Script to run npm test 50 times sequentially

echo "Starting 50 sequential test runs..."
echo "======================================"

for i in {1..50}
do
    echo "\n--- Test Run #$i ---"
    echo "$(date): Starting test run $i"
    
    npm run test
    
    if [ $? -eq 0 ]; then
        echo "$(date): Test run $i completed successfully"
    else
        echo "$(date): Test run $i failed with exit code $?"
    fi
    
    echo "--- End of Test Run #$i ---\n"
done

echo "======================================"
echo "All 20 test runs completed!"
echo "$(date): Script finished"