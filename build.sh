#!/bin/bash

go build -o msm-client main.go

if [ $? -ne 0 ]; then
    echo "Build failed. Please check the output for errors."
    exit 1
fi

echo "Build successful. Executable created: msm-client"

tar -czf msm-client.tar.gz msm-client templates

if [ $? -ne 0 ]; then
    echo "Packaging failed. Please check the output for errors."
    exit 1
fi

echo "Packaging successful. Archive created: msm-client.tar.gz"