{
  inputs = {
    nixpkgs = {
      url = "github:NixOS/nixpkgs/nixos-26.05";
      flake = false;
    };
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems =
        f:
        builtins.listToAttrs (
          map (system: {
            name = system;
            value = f system;
          }) systems
        );
      mkPkgs =
        system:
        import nixpkgs {
          inherit system;
          config = {
            allowUnfree = true;
          };
        };
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = mkPkgs system;
          smallctl = pkgs.buildGoModule {
            pname = "smallctl";
            version = "1.1.1";
            src = self;
            vendorHash = "sha256-AVoMypzpvdkm4qiSOs4JLiBoCwG6+N/phwqZt/WTCr8=";
          };
        in
        {
          inherit smallctl;
          default = smallctl;
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = mkPkgs system;
        in
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.go
              pkgs.gopls
              pkgs.libnotify
            ];
          };
        }
      );
    };
}
