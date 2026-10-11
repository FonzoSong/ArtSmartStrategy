FROM python:3.12-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends fonts-noto-cjk fonts-droid-fallback && rm -rf /var/lib/apt/lists/*
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY backend ./backend
COPY migrations ./migrations
COPY alembic.ini .
COPY frontend ./frontend
COPY scripts ./scripts
COPY rules ./rules
COPY tests/fixtures ./tests/fixtures
RUN mkdir -p /app/data
VOLUME ["/app/data"]
EXPOSE 8000
CMD ["python", "-m", "backend.container_entrypoint"]
