FROM python:3.12-slim-bookworm@sha256:34386ef0cb081344d7ec1c103ba398e6e9f64e9ab3a1509accc92a4e24a07258
WORKDIR /app
COPY runtime-requirements.txt ./
RUN pip install --no-cache-dir -r runtime-requirements.txt
COPY common.py predict.py train.py cpu.py ./
USER 65532:65532
ENV OMP_NUM_THREADS=1 OPENBLAS_NUM_THREADS=1 TOKENIZERS_PARALLELISM=false PYTHONDONTWRITEBYTECODE=1
ENTRYPOINT ["python", "/app/cpu.py"]
