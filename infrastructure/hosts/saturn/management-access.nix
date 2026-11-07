{ ... }: {
  users.users.root.openssh.authorizedKeys.keys = [
    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAdm4iJJvXlnvitJFj8mKzxBIzmxRXcTPTZoWV3wzFm/ mesh@venus-20260731"
  ];
  users.users.devon.openssh.authorizedKeys.keys = [
    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAdm4iJJvXlnvitJFj8mKzxBIzmxRXcTPTZoWV3wzFm/ mesh@venus-20260731"
  ];
}
