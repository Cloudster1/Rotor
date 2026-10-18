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
