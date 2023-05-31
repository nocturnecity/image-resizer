FROM --platform=linux/amd64  dpokidov/imagemagick:latest

RUN apt-get update && \
    apt-get install -y python3-pip curl unzip && \
    rm -rf /var/lib/apt/lists/*

RUN pip3 install awscli fastapi uvicorn boto3

COPY app.py app.py

EXPOSE 8000

ENTRYPOINT ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8000"]
