# Model catalog federation

This repository supports two manual actions for the shared model catalog:

1. Refresh pricing metadata
   - Triggered from the GitHub Actions UI.
   - Runs the pricing refresh script against the shared catalog.
   - Updates the catalog file in the repository.

2. Federate share to all copilots
   - Triggered from the GitHub Actions UI.
   - Publishes the current shared catalog artifact for downstream copilots.
   - The current implementation is a placeholder that can be connected to a package registry, release artifact, or deployment endpoint in a later step.

## Manual workflow

Open the Actions tab in GitHub and run the workflow named "Model Catalog Manual Actions".
Select either:
- refresh-pricing
- federate-share
