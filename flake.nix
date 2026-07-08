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
      system = "x86_64-linux";
      pkgs = import nixpkgs {
        inherit system;
        config = {
          allowUnfree = true;
        };
      };
      packages = [
        pkgs.go
        pkgs.gopls
        pkgs.libnotify
      ];
    in
    {
      devShells.${system}.default = pkgs.mkShell {
        packages = packages;
      };
    };
}
