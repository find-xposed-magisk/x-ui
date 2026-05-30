class DBOutbound {

    constructor(data) {
        this.id = 0;
        this.up = 0;
        this.down = 0;
        this.sort = 0;
        this.sendThrough = "";
        this.protocol = "";
        this.settings = "";
        this.tag = "";
        this.streamSettings = "";
        this.proxySettings = "";
        this.mux = "";
        this.targetStrategy = "";
        if (data == null) {
            return;
        }
        ObjectUtil.cloneProps(this, data);
    }

    toOutbound() {
        const config = {
            protocol: this.protocol,
            tag: this.tag,
        };
        if (!ObjectUtil.isEmpty(this.sendThrough)) {
            config.sendThrough = this.sendThrough;
        }
        if (!ObjectUtil.isEmpty(this.settings)) {
            config.settings = JSON.parse(this.settings);
        }
        if (!ObjectUtil.isEmpty(this.streamSettings)) {
            config.streamSettings = JSON.parse(this.streamSettings);
        }
        if (!ObjectUtil.isEmpty(this.proxySettings)) {
            config.proxySettings = JSON.parse(this.proxySettings);
        }
        if (!ObjectUtil.isEmpty(this.mux)) {
            config.mux = JSON.parse(this.mux);
        }
        if (!ObjectUtil.isEmpty(this.targetStrategy)) {
            config.targetStrategy = this.targetStrategy;
        }
        return Outbound.fromJson(config);
    }

    static payloadFromOutbound(outbound, db) {
        const json = outbound.toJson();
        const data = {
            up: db.up,
            down: db.down,
            sendThrough: json.sendThrough || "",
            protocol: json.protocol,
            tag: json.tag || "",
            targetStrategy: json.targetStrategy || "",
            settings: JSON.stringify(json.settings ?? {}, null, 2),
            streamSettings: json.streamSettings ? JSON.stringify(json.streamSettings, null, 2) : "",
            proxySettings: json.proxySettings ? JSON.stringify(json.proxySettings, null, 2) : "",
            mux: json.mux ? JSON.stringify(json.mux, null, 2) : "",
        };
        return data;
    }
}
