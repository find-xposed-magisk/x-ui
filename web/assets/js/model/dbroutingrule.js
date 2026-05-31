class DBRoutingRule {

    constructor(data) {
        this.id = 0;
        this.tag = "";
        this.sort = 0;
        this.rawJson = "";
        if (data == null) {
            return;
        }
        ObjectUtil.cloneProps(this, data);
    }

    toRule() {
        const rule = this.rawJson ? JSON.parse(this.rawJson) : { type: "field" };
        return rule;
    }

    toTableRow() {
        const r = this.toRule();
        const row = { id: this.id, tag: this.tag, sort: this.sort, key: this.id, ...r };
        if (row.domain) row.domain = Array.isArray(row.domain) ? row.domain.join(',') : row.domain;
        if (row.ip) row.ip = Array.isArray(row.ip) ? row.ip.join(',') : row.ip;
        if (row.source) row.source = Array.isArray(row.source) ? row.source.join(',') : row.source;
        if (row.user) row.user = Array.isArray(row.user) ? row.user.join(',') : row.user;
        if (row.inboundTag) row.inboundTag = Array.isArray(row.inboundTag) ? row.inboundTag.join(',') : row.inboundTag;
        if (row.protocol) row.protocol = Array.isArray(row.protocol) ? row.protocol.join(',') : row.protocol;
        if (row.attrs) row.attrs = JSON.stringify(row.attrs, null, 2);
        return row;
    }

    static payloadFromRule(rule, db) {
        const copy = JSON.parse(JSON.stringify(rule));
        delete copy.ruleTag;
        return {
            id: db.id,
            tag: db.tag || copy.ruleTag || "",
            rawJson: JSON.stringify(copy),
        };
    }
}
