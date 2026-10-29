# Learn suspicious behaviour from addresses Tether later froze

The learned score of ADR-0015 now learns what suspicious behaviour looks like from addresses Tether froze after a past window, much as a recommender learns a user's taste from the videos they liked. It still runs alongside the deterministic rules and is still not the published Risk Score.

The Isolation Forest was measuring sampling, not suspicion. Its validation baseline was sampled by walking back from the window end, so every baseline address had transacted in the window's final hour. Frozen addresses had not, and time since last transfer alone separated the two with ROC-AUC 0.998. Against a control group sampled at random times across the window (`crawl.py --spread`), the Isolation Forest scored 0.536. An unsupervised model finds the unusual, and unusual is not suspicious.

Training fits two learners and keeps whichever validates better. `GmmScorer` sees only suspicious addresses and learns their distribution, like content-based filtering. `GbdtScorer` adds randomly drawn unlabeled addresses as weak negatives, like negative sampling on implicit feedback. "Unlabeled" means not yet flagged, not confirmed benign. Validation always trains on older 30-day windows and tests on the next one, which is how the model is used once deployed. Across nine such tests from May 2025 to September 2026, GBDT scored ROC-AUC 0.903 to 0.950 and GMM 0.770 to 0.826; doubling the training history moved neither by more than 0.02. With amount, transfer count and counterparty count removed, GMM still scored 0.760, so the model learns behaviour rather than size. A rank-averaged ensemble chosen on one split lost to GBDT alone on every later window and was dropped.

Training refuses to save a model when time since last transfer alone predicts the label (mean ROC-AUC outside 0.5 ± 0.1), because that is the sampling artifact coming back. Labels exclude addresses Tether later unfroze; 11% of all historical freezes were reversed.

The transfer cap drops from 10,000 to 1,000 for both training and scoring. Every model was validated at 1,000, scoring then needs at most five provider pages, and older crawls at 10,000 reduce exactly to it by keeping their first 1,000 records.

Neighbour features and a GraphSAGE encoder were tested and not adopted. On a dense 53,000-address graph, a two-layer GraphSAGE trained with Deep Graph Infomax did not beat a plain average of neighbour features. Neighbour features did not improve the address-level score either.

## Consequences

The model learns what Tether freezes, which is not necessarily fraud: some frozen addresses hold stolen funds rather than belong to thieves, and Tether acts on reported, large cases. Testing against an independent source such as scam reports is still open.

The raw score's scale depends on the chosen learner (a 0–1 ranking for GBDT, a log density for GMM), so people read the percentile against held-out unlabeled addresses. Retraining needs new windows whose 60-day label horizon has closed.
