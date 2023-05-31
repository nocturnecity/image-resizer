import subprocess

from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI()


class Command(BaseModel):
    command: str


@app.post("/execute/")
async def execute(cmd: Command):
    # Split command into parts
    cmd_parts = cmd.command.split()

    # Run the command and capture output
    result = subprocess.run(cmd_parts, capture_output=True, text=True)

    # If the command was successful, return the stdout
    if result.returncode == 0:
        return {"output": result.stdout}
    else:
        return {"error": result.stderr}
