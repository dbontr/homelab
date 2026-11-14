# Optional trained Alien backend

The [Alien Space of Science](https://github.com/alejandrohdez00/alien-space-of-science) implementation is an MIT-licensed research pipeline associated with [the paper](https://arxiv.org/abs/2603.01092). It uses learned coherence and author-atom availability models to rank combinations extracted from a corpus of AI research papers. The baseline `ideas` skill is not that model and must not claim equivalent training, validation, or cognitive-unavailability estimates.

## Use only for an explicitly requested model-assisted run

1. Check whether the executing environment can run Python 3.11+, PyTorch, and the repository, and has adequate disk and memory. Sampling can use CPU or CUDA; training the full availability model needs substantially greater compute.
2. Identify the relevant corpus. Published checkpoints were trained on an ML-oriented corpus with 16,068 papers; they are not valid general-purpose evaluators for mathematical physics, electrical engineering, or unrelated subject matter. Transfer to another corpus requires atomization, clustering, dataset reconstruction, and retraining.
3. Before downloading large datasets, using hosted inference APIs, retraining, or allocating shared GPU resources, confirm that the user has requested the action and its resource or budget implications. Never assume a key or access permission exists.
4. Consult the current upstream README and DATA.md before executing commands. Do not vendor model weights or source into the skill bundle; keep the original project's license and attribution intact.
5. Distinguish published model output from custom heuristic rankings. Cite the original paper and any actual sources examined when discussing novelty.

The following commands execute the original external research implementation,
which depends on Python and PyTorch. They are not bundled scripts in this
skill. The skill's own deterministic ranking utility is written in Go and
has no Python dependency. Do not claim that the released trained model is
available through the Go ranker.

## Example upstream commands

Run inside an authorized checkout of the original repository after installing requirements and retrieving the matching checkpoint files from the upstream Hugging Face release:

```bash
python alien.py sample --n-gen 10000 --betas 0.4 0.7 --top-k 100 --output-dir results/sweep
```

For reconstruction, configure a supported LLM provider and its API credentials through approved environment configuration, then use the appropriate upstream command:

```bash
python alien.py reconstruct --method inference --results results/sweep/k3/beta_0.7/results.json --top-k 3 --reconstruction-model gemini/gemini-3.1-pro-preview
```

Reconstruction makes provider API calls. The released model/inference artifacts total approximately 358 MB, excluding the corpus and training data. The [official artifact collection](https://huggingface.co/collections/alejandrohdez/alien-space-of-science-6ad8b17d0399975c463c6e1e) and [data README](https://github.com/alejandrohdez00/alien-space-of-science/blob/main/DATA.md) describe the precise artifacts.

The baseline `ideas` procedure remains available with no checkpoint or specialized compute. Its ratings are transparent human/LLM heuristics, not replicates of the authors' reported model estimates.
