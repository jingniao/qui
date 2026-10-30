// Frozen pre-U4 list payload builder, including edit merge and forward clearing.
export function legacyList(selfHostedForm: any, inb: any = null) {
 const editingInbound = { value: inb }; const srvId = selfHostedForm.serverId; let payload: any;
 const applyForwardSettings = (settings: any, protocol: string) => { if(protocol !== 'forward') return; for(const key of Object.keys(settings)) delete settings[key]; settings.forward = {...selfHostedForm.forward}; };
    if (selfHostedForm.inboundId && editingInbound.value) {
      const inb = editingInbound.value;
      const settings: any = { ...(inb.settings || {}) };
      const proto = inb.protocol;
      if (proto === 'wireguard') {
        settings.transport = '';
      } else if (proto === 'hysteria2' || proto === 'tuic') {
        // leave transport unset as per old page behavior
      } else {
        settings.transport = selfHostedForm.transport;
      }
      if (proto === 'trojan' || proto === 'hysteria2' || proto === 'tuic') {
        settings.security = 'tls';
      } else if (proto === 'wireguard') {
        // keep as is
      } else {
        settings.security = selfHostedForm.security;
      }
      if (settings.security === 'tls') {
        settings.server_name = selfHostedForm.server_name || undefined;
        if (selfHostedForm.certificate_pem) settings.certificate_pem = selfHostedForm.certificate_pem;
        if (selfHostedForm.private_key_pem) settings.private_key_pem = selfHostedForm.private_key_pem;
        if (selfHostedForm.trusted_ca_pem) settings.trusted_ca_pem = selfHostedForm.trusted_ca_pem;
      }
      if (selfHostedForm.transport === 'ws') {
        settings.path = selfHostedForm.path || undefined;
      }
      if (selfHostedForm.transport === 'grpc') {
        settings.service_name = selfHostedForm.service_name || undefined;
      }
      if (proto === 'shadowsocks') {
        settings.method = selfHostedForm.method;
      }
      applyForwardSettings(settings,proto);
      payload = {
        name: selfHostedForm.name.trim(),
        nodeId: inb.nodeId,
        protocol: inb.protocol,
        listen: inb.listen,
        port: selfHostedForm.port,
        enabled: inb.enabled,
        subscriptionVisible: inb.subscriptionVisible,
        internalRelay: inb.internalRelay,
        defaultOutbound: inb.defaultOutbound,
        allowedUserIds: inb.allowedUserIds,
        settings: settings
      };
      return payload;
    } else {
      const proto = selfHostedForm.protocol;
      let security = selfHostedForm.security;
      if (proto === 'trojan' || proto === 'hysteria2' || proto === 'tuic') security = 'tls';
      const settings: any = {};
      if (proto === 'wireguard') {
        settings.transport = '';
      } else if (proto === 'hysteria2' || proto === 'tuic') {
        // leave unset
      } else {
        settings.transport = selfHostedForm.transport;
      }
      settings.security = proto === 'wireguard' ? undefined : security;
      if (security === 'tls') {
        settings.server_name = selfHostedForm.server_name || undefined;
        if (selfHostedForm.certificate_pem) settings.certificate_pem = selfHostedForm.certificate_pem;
        if (selfHostedForm.private_key_pem) settings.private_key_pem = selfHostedForm.private_key_pem;
        if (selfHostedForm.trusted_ca_pem) settings.trusted_ca_pem = selfHostedForm.trusted_ca_pem;
      }
      if (selfHostedForm.transport === 'ws') settings.path = selfHostedForm.path || undefined;
      if (selfHostedForm.transport === 'grpc') settings.service_name = selfHostedForm.service_name || undefined;
      if (proto === 'shadowsocks') settings.method = selfHostedForm.method;
      applyForwardSettings(settings,proto);
      payload = {
        name: selfHostedForm.name.trim(),
        nodeId: srvId,
        protocol: proto,
        listen: '0.0.0.0',
        port: selfHostedForm.port,
        enabled: true,
        subscriptionVisible: proto !== 'forward',
        internalRelay: false,
        defaultOutbound: 'direct',
        allowedUserIds: [],
        settings: settings
      };
      return payload;
    }
}
