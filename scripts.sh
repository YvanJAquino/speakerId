#!/bin/bash

docker build -t gcr.io/$DEVSHELL_PROJECT_ID/sfdc-oauth2 . 
docker push gcr.io/$DEVSHELL_PROJECT_ID/sfdc-oauth2
gcloud alpha run deploy sfdc-oauth2 \
    --project $DEVSHELL_PROJECT_ID \
    --set-env-vars PROJECT_ID=$DEVSHELL_PROJECT_ID \
    --set-env-vars ENVIRONMENT=PRODUCTION \
    --image gcr.io/$DEVSHELL_PROJECT_ID/sfdc-oauth2 \
    --timeout 5m \
    --no-cpu-throttling \
    --region us-central1 \
    --platform managed \
    --min-instances 0 \
    --max-instances 5 \
    --allow-unauthenticated
    