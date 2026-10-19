# Kaggle email dataset candidate review

These four user-suggested Kaggle datasets are **not included** in the current
classification-objective experiment. Keeping its evidence, labels and splits
fixed isolates the training-objective change. This is a review of publisher
metadata, not a blanket rejection of Kaggle or copyleft-licensed data.

Public Kaggle API metadata was checked on October 19, 2026. Each dataset was
version 1. The license names below are the publisher's declarations.

| Dataset | Declared license | Next checks before adding |
|---|---|---|
| [naserabdullahalam/phishing-email-dataset](https://www.kaggle.com/datasets/naserabdullahalam/phishing-email-dataset) | CC BY-SA 4.0 | The card combines Enron, Ling, CEAS, Nazario, Nigerian Fraud and SpamAssassin. It conflates spam with phishing. Preserve source identities, check original grants, remove overlap with our existing Nazario data, separate ordinary spam from phishing and group campaigns before splitting. |
| [llabhishekll/fraud-email-dataset](https://www.kaggle.com/datasets/llabhishekll/fraud-email-dataset) | Unknown | The description invites use and cites the CLAIR fraud corpus through another Kaggle upload. Trace the original collection's grant and keep fraud labels distinct from ordinary spam. An Unknown field alone does not prove that no permission exists. |
| [subhajournal/phishingemails](https://www.kaggle.com/datasets/subhajournal/phishingemails) | GNU Lesser General Public License 3.0 | Identify the original collections, duplicates and label definitions, and document the conditions for the dataset artifacts we distribute. LGPL is not automatically incompatible with an open-source Thura deployment. |
| [harshalpatil3558/spam-mail-prediction-dataset](https://www.kaggle.com/datasets/harshalpatil3558/spam-mail-prediction-dataset) | CC0 Public Domain | The card describes spam/non-spam but does not identify the original collection. Confirm the actual source and email-versus-SMS composition; review spam for phishing contamination before treating it as ordinary spam. |

Free download, permitted research use, commercial use and redistribution are
different questions. A dataset license does not by itself determine every
obligation for a trained model, and a mirror's metadata does not establish that
third-party source rights changed. [CC0 permits commercial reuse of rights the
dedicator can waive and expressly preserves others' rights.](https://creativecommons.org/publicdomain/zero/1.0/)
Copyleft conditions should be assessed for the actual artifact, rather than used
as an automatic exclusion.

The current sources remain the pinned LLMGen English synthetic set, the original
Nazario phishing collection, conservatively selected Guenter spam and original
training contrasts/hard negatives. See `scripts/mail-classifier-training/README.md`
and `scripts/mail-llama-training/README.md` for grants, hashes and limitations.
Representative legitimate mail remains an important gap; a larger binary
spam/phishing aggregate is not automatically a reliable three-class replacement.
