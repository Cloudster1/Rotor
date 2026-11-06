"""The generated protocol code imports from inside the package and round-trips a message."""

from rotor_runner.pb.rotor.runner.v1 import runner_pb2, runner_pb2_grpc


def test_a_hello_survives_the_wire() -> None:
    sent = runner_pb2.ConnectRequest(seq=1, hello=runner_pb2.Hello(bootstrap_token="b", runner_version="0.1.0"))
    received = runner_pb2.ConnectRequest.FromString(sent.SerializeToString())
    assert received.WhichOneof("message") == "hello"
    assert received.hello.bootstrap_token == "b"
    assert received.hello.WhichOneof("credential") == "bootstrap_token"


def test_the_service_stub_is_generated() -> None:
    assert hasattr(runner_pb2_grpc, "RunnerControlServiceStub")


def test_a_skill_file_request_and_its_answer_survive_the_wire() -> None:
    asked = runner_pb2.ConnectRequest(
        skill_file=runner_pb2.SkillFileRequest(request_id="q", name="tdd", version=2, path="SKILL.md", offset=7)
    )
    received = runner_pb2.ConnectRequest.FromString(asked.SerializeToString())
    assert received.WhichOneof("message") == "skill_file"
    assert (received.skill_file.name, received.skill_file.version, received.skill_file.offset) == ("tdd", 2, 7)
    answer = runner_pb2.ConnectResponse(
        skill_file=runner_pb2.SkillFileResponse(request_id="q", content="téxt", offset=7, total=12)
    )
    back = runner_pb2.ConnectResponse.FromString(answer.SerializeToString())
    assert back.WhichOneof("message") == "skill_file"
    assert (back.skill_file.content, back.skill_file.total, back.skill_file.refusal) == ("téxt", 12, "")


def test_start_run_carries_each_agents_skills_by_node() -> None:
    start = runner_pb2.StartRun(run_id="r")
    card = start.agent_skills["analyse"].skills.add(name="tdd", version=2, description="Test first.")
    card.files.add(path="SKILL.md", bytes=120)
    back = runner_pb2.StartRun.FromString(start.SerializeToString())
    files = back.agent_skills["analyse"].skills[0].files
    assert [(f.path, f.bytes) for f in files] == [("SKILL.md", 120)]
