{
  lib,
  buildGoModule,
  fetchFromGitHub,
  go_1_27,
}:
(buildGoModule.override { go = go_1_27; }) (finalAttrs: {
  pname = "queryshift";
  version = "0.8.1";

  src = fetchFromGitHub {
    owner = "sarankumar1325";
    repo = "queryshift";
    tag = "v${finalAttrs.version}";
    hash = "sha256-QJWJTSS92g7Ay2r5yckbRX5/JOUIBDTKGXpEzerZyKI=";
  };

  vendorHash = "sha256-iN6SE0ehjVVXkw8hHsUrCNXPV1Xchw6gxt92kVCQTV4=";

  subPackages = [ "cmd/queryshift" ];

  env.CGO_ENABLED = "0";

  ldflags = [
    "-X main.version=${finalAttrs.version}"
  ];

  doCheck = true;

  meta = {
    description = "In-database observability for PostgreSQL";
    longDescription = ''
      queryshift is a read-only PostgreSQL observability CLI. A single static
      binary connects with a read-only role, reads Postgres's own
      statistics views, and prints findings-first health reports plus
      diffs against previous runs. No agent, no external service, no
      write privilege anywhere in the path.
    '';
    homepage = "https://github.com/sarankumar1325/queryshift";
    changelog = "https://github.com/sarankumar1325/queryshift/releases/tag/v${finalAttrs.version}";
    license = lib.licenses.asl20;
    mainProgram = "queryshift";
  };
})
