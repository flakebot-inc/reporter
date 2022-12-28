#!/bin/bash

# This script is used to invalidate the CloudFront cache for the given distribution
# Usage: ./release-cloudfront.sh <distribution-id>

# Check if the distribution id is provided
if [ -z "$1" ]
then
  echo "Please provide the distribution id"
  exit 1
fi

# Get most recent version directory in s3 that doesn't contain "alpha" or latest
LATEST_VERSION=$(aws s3 ls s3://get-flakebot/reporter/ | grep PRE | grep -v alpha | grep -v latest | sort | tail -n 1 | awk '{print $2}' | sed 's/.$//')
echo "Latest version: $LATEST_VERSION"

if [ -z "$LATEST_VERSION" ]
then
  echo "No version found"
  exit 0
fi

# Copy the latest version to the latest directory
aws s3 cp s3://get-flakebot/reporter/$LATEST_VERSION s3://get-flakebot/reporter/latest --recursive
                
# Invalidate the CloudFront cache for the latest directory
aws cloudfront create-invalidation --distribution-id $1 --paths "/reporter/latest*"