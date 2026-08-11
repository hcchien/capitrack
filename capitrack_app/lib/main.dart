import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

void main() => runApp(const CapiTrackApp());

class CapiTrackApp extends StatefulWidget {
  const CapiTrackApp({super.key});
  @override
  State<CapiTrackApp> createState() => _CapiTrackAppState();
}

class _CapiTrackAppState extends State<CapiTrackApp> {
  ThemeMode mode = ThemeMode.system;
  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'CapiTrack',
    debugShowCheckedModeBanner: false,
    themeMode: mode,
    theme: appTheme(Brightness.light),
    darkTheme: appTheme(Brightness.dark),
    home: HomePage(
      onTheme: () => setState(() {
        mode = mode == ThemeMode.dark ? ThemeMode.light : ThemeMode.dark;
      }),
    ),
  );
}

ThemeData appTheme(Brightness brightness) {
  const seed = Color(0xff146c4c);
  final scheme = ColorScheme.fromSeed(seedColor: seed, brightness: brightness);
  return ThemeData(
    colorScheme: scheme,
    useMaterial3: true,
    scaffoldBackgroundColor: brightness == Brightness.light
        ? const Color(0xfff7f5ef)
        : const Color(0xff111714),
    cardTheme: const CardThemeData(elevation: 0, margin: EdgeInsets.zero),
    inputDecorationTheme: const InputDecorationTheme(
      border: OutlineInputBorder(),
    ),
  );
}

class Api {
  Api(this.baseUrl);
  String baseUrl;
  Uri uri(String path) =>
      Uri.parse('${baseUrl.replaceAll(RegExp(r'/$'), '')}$path');

  Future<dynamic> send(
    String path, {
    String method = 'GET',
    Map<String, dynamic>? body,
  }) async {
    final headers = {'Content-Type': 'application/json'};
    late http.Response response;
    final target = uri(path);
    switch (method) {
      case 'POST':
        response = await http.post(
          target,
          headers: headers,
          body: jsonEncode(body ?? {}),
        );
      case 'PUT':
        response = await http.put(
          target,
          headers: headers,
          body: jsonEncode(body ?? {}),
        );
      case 'DELETE':
        response = await http.delete(target, headers: headers);
      default:
        response = await http.get(target, headers: headers);
    }
    if (response.statusCode < 200 || response.statusCode >= 300) {
      var message = 'HTTP ${response.statusCode}';
      try {
        final decoded = jsonDecode(response.body);
        message = decoded is Map
            ? '${decoded['error'] ?? decoded['message'] ?? message}'
            : message;
      } catch (_) {}
      throw Exception(message);
    }
    if (response.body.trim().isEmpty) return null;
    return jsonDecode(response.body);
  }
}

class HomePage extends StatefulWidget {
  const HomePage({super.key, required this.onTheme});
  final VoidCallback onTheme;
  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  final api = Api('http://localhost:8080');
  int page = 0;
  bool loading = true;
  String? error;
  Map<String, dynamic> portfolio = {};
  List<Map<String, dynamic>> transactions = [], cash = [], cashTx = [];
  List<Map<String, dynamic>> liabilities = [],
      liabilityTx = [],
      alerts = [],
      events = [];

  static const destinations = [
    ('總覽', Icons.pie_chart_outline),
    ('交易', Icons.swap_horiz),
    ('現金', Icons.account_balance_wallet_outlined),
    ('負債', Icons.credit_card),
    ('提醒', Icons.notifications_none),
  ];

  @override
  void initState() {
    super.initState();
    reload();
  }

  Future<void> reload() async {
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final values = await Future.wait([
        api.send('/api/portfolio'),
        api.send('/api/transactions'),
        api.send('/api/cash-accounts'),
        api.send('/api/cash-transactions'),
        api.send('/api/liabilities'),
        api.send('/api/liability-transactions'),
        api.send('/api/alerts'),
        api.send('/api/alert-events'),
      ]);
      if (!mounted) return;
      setState(() {
        portfolio = Map<String, dynamic>.from(values[0]);
        transactions = maps(values[1]);
        cash = maps(values[2]);
        cashTx = maps(values[3]);
        liabilities = maps(values[4]);
        liabilityTx = maps(values[5]);
        alerts = maps(values[6]);
        events = maps(values[7]);
        loading = false;
      });
    } catch (e) {
      if (mounted) {
        setState(() {
          error = cleanError(e);
          loading = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final wide = MediaQuery.sizeOf(context).width >= 800;
    final content = SafeArea(
      child: Column(
        children: [
          _header(),
          Expanded(
            child: loading
                ? const Center(child: CircularProgressIndicator())
                : error != null
                ? _connectionError()
                : RefreshIndicator(onRefresh: reload, child: _page()),
          ),
        ],
      ),
    );
    return Scaffold(
      body: wide
          ? Row(
              children: [
                NavigationRail(
                  extended: MediaQuery.sizeOf(context).width >= 1100,
                  selectedIndex: page,
                  onDestinationSelected: (v) => setState(() => page = v),
                  leading: const Padding(
                    padding: EdgeInsets.all(16),
                    child: Icon(Icons.show_chart, size: 34),
                  ),
                  destinations: [
                    for (final d in destinations)
                      NavigationRailDestination(
                        icon: Icon(d.$2),
                        label: Text(d.$1),
                      ),
                  ],
                ),
                const VerticalDivider(width: 1),
                Expanded(child: content),
              ],
            )
          : content,
      bottomNavigationBar: wide
          ? null
          : NavigationBar(
              selectedIndex: page,
              onDestinationSelected: (v) => setState(() => page = v),
              destinations: [
                for (final d in destinations)
                  NavigationDestination(icon: Icon(d.$2), label: d.$1),
              ],
            ),
      floatingActionButton: error == null ? _fab() : null,
    );
  }

  Widget _header() => Padding(
    padding: const EdgeInsets.fromLTRB(20, 10, 8, 8),
    child: Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                'CAPITRACK',
                style: TextStyle(
                  fontSize: 11,
                  letterSpacing: 2,
                  color: Theme.of(context).colorScheme.primary,
                ),
              ),
              Text(
                destinations[page].$1,
                style: Theme.of(context).textTheme.headlineSmall?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
          ),
        ),
        IconButton(
          tooltip: '重新載入',
          onPressed: reload,
          icon: const Icon(Icons.refresh),
        ),
        IconButton(
          tooltip: '外觀',
          onPressed: widget.onTheme,
          icon: const Icon(Icons.contrast),
        ),
        IconButton(
          tooltip: '連線設定',
          onPressed: _settings,
          icon: const Icon(Icons.settings_outlined),
        ),
      ],
    ),
  );

  Widget _connectionError() => Center(
    child: Padding(
      padding: const EdgeInsets.all(28),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.cloud_off_outlined, size: 56),
          const SizedBox(height: 16),
          Text(
            '無法連線到 CapiTrack',
            style: Theme.of(context).textTheme.titleLarge,
          ),
          const SizedBox(height: 8),
          Text(error!, textAlign: TextAlign.center),
          const SizedBox(height: 18),
          FilledButton.icon(
            onPressed: _settings,
            icon: const Icon(Icons.settings),
            label: const Text('設定伺服器'),
          ),
        ],
      ),
    ),
  );

  Widget _page() => switch (page) {
    0 => _overview(),
    1 => _transactions(),
    2 => _cash(),
    3 => _liabilities(),
    _ => _alerts(),
  };

  Widget _scroll(List<Widget> children) => ListView(
    padding: const EdgeInsets.fromLTRB(20, 8, 20, 100),
    children: children,
  );
  String get currency => '${portfolio['baseCurrency'] ?? 'TWD'}';
  String money(dynamic value, [String? unit]) =>
      '${unit ?? currency} ${numValue(value).toStringAsFixed(2)}';

  Widget _overview() {
    final s = map(portfolio['summary']);
    final holdings = maps(portfolio['holdings']);
    return _scroll([
      Wrap(
        spacing: 12,
        runSpacing: 12,
        children: [
          metric('淨資產', money(s['netWorth']), Icons.account_balance),
          metric('總資產', money(s['totalValue']), Icons.trending_up),
          metric('現金', money(s['totalCash']), Icons.savings_outlined),
          metric('總負債', money(s['totalLiabilities']), Icons.trending_down),
          metric(
            '未實現損益',
            signedMoney(s['unrealized']),
            Icons.auto_graph,
            color: gainColor(numValue(s['unrealized'])),
          ),
          metric(
            '已實現損益',
            signedMoney(s['realized']),
            Icons.done_all,
            color: gainColor(numValue(s['realized'])),
          ),
        ],
      ),
      const SizedBox(height: 24),
      sectionTitle('持股', '${holdings.length} 個標的'),
      if (holdings.isEmpty) empty('尚無持股，新增第一筆交易吧'),
      for (final h in holdings) holdingCard(h),
      const SizedBox(height: 18),
      FilledButton.icon(
        onPressed: _refreshMarket,
        icon: const Icon(Icons.sync),
        label: const Text('更新行情與匯率'),
      ),
    ]);
  }

  Widget metric(String label, String value, IconData icon, {Color? color}) =>
      SizedBox(
        width: 210,
        child: Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Icon(icon, size: 18),
                    const SizedBox(width: 7),
                    Text(label),
                  ],
                ),
                const SizedBox(height: 10),
                Text(
                  value,
                  style: Theme.of(context).textTheme.titleLarge?.copyWith(
                    fontWeight: FontWeight.w700,
                    color: color,
                  ),
                ),
              ],
            ),
          ),
        ),
      );

  Widget holdingCard(Map<String, dynamic> h) => Card(
    child: ListTile(
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      title: Text(
        '${h['name']}',
        style: const TextStyle(fontWeight: FontWeight.w700),
      ),
      subtitle: Text(
        '${h['symbol']} · ${h['quantity']} 股 · 均價 ${money(h['averageCost'], '${h['currency']}')}',
      ),
      trailing: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Text(
            money(h['marketValue']),
            style: const TextStyle(fontWeight: FontWeight.w700),
          ),
          Text(
            '${numValue(h['unrealizedPercent']).toStringAsFixed(2)}%',
            style: TextStyle(color: gainColor(numValue(h['unrealized']))),
          ),
        ],
      ),
      onTap: () => _price(h),
    ),
  );

  Widget _transactions() => _scroll([
    sectionTitle('交易紀錄', '${transactions.length} 筆'),
    if (transactions.isEmpty) empty('尚無交易紀錄'),
    for (final t in transactions)
      dismissible(
        key: 'tx-${t['id']}',
        onDelete: () =>
            mutate('/api/transactions/${t['id']}', method: 'DELETE'),
        child: Card(
          child: ListTile(
            leading: CircleAvatar(
              child: Text('${t['type']}' == 'buy' ? '買' : '賣'),
            ),
            title: Text(
              '${t['name']} · ${t['symbol']}',
              style: const TextStyle(fontWeight: FontWeight.w600),
            ),
            subtitle: Text(
              '${t['tradedAt']}  ${t['quantity']} × ${money(t['price'], '${t['currency']}')}\n${t['note'] ?? ''}',
            ),
            isThreeLine: (t['note'] ?? '').toString().isNotEmpty,
            trailing: Text(
              money(
                numValue(t['quantity']) * numValue(t['price']),
                '${t['currency']}',
              ),
            ),
            onTap: () => _trade(existing: t),
          ),
        ),
      ),
  ]);

  Widget _cash() => _scroll([
    Row(
      children: [
        Expanded(child: sectionTitle('現金帳戶', '${cash.length} 個')),
        TextButton.icon(
          onPressed: _cashAccount,
          icon: const Icon(Icons.add),
          label: const Text('帳戶'),
        ),
      ],
    ),
    for (final a in cash)
      Card(
        child: ListTile(
          leading: const CircleAvatar(
            child: Icon(Icons.account_balance_wallet_outlined),
          ),
          title: Text(
            '${a['name']}',
            style: const TextStyle(fontWeight: FontWeight.w600),
          ),
          subtitle: Text(
            '${a['currency']}${a['hidden'] == true ? ' · 已隱藏' : ''}',
          ),
          trailing: Text(
            money(a['balance'], '${a['currency']}'),
            style: const TextStyle(fontWeight: FontWeight.w700),
          ),
          onTap: () => _cashEntry(account: a),
        ),
      ),
    const SizedBox(height: 22),
    sectionTitle('現金流水', '${cashTx.length} 筆'),
    for (final t in cashTx)
      dismissible(
        key: 'cash-${t['id']}',
        onDelete: () =>
            mutate('/api/cash-transactions/${t['id']}', method: 'DELETE'),
        child: Card(
          child: ListTile(
            title: Text('${t['name']} · ${cashType(t['type'])}'),
            subtitle: Text('${t['tradedAt']}  ${t['note'] ?? ''}'),
            trailing: Text(money(t['amount'], '${t['currency']}')),
          ),
        ),
      ),
  ]);

  Widget _liabilities() => _scroll([
    Row(
      children: [
        Expanded(child: sectionTitle('負債項目', '${liabilities.length} 個')),
        TextButton.icon(
          onPressed: _liability,
          icon: const Icon(Icons.add),
          label: const Text('負債'),
        ),
      ],
    ),
    for (final a in liabilities)
      Card(
        child: ListTile(
          leading: const CircleAvatar(child: Icon(Icons.credit_card)),
          title: Text(
            '${a['name']}',
            style: const TextStyle(fontWeight: FontWeight.w600),
          ),
          subtitle: Text('${a['category']} · 年利率 ${a['interestRate']}%'),
          trailing: Text(
            money(a['balance'], '${a['currency']}'),
            style: const TextStyle(fontWeight: FontWeight.w700),
          ),
          onTap: () => _liabilityEntry(item: a),
        ),
      ),
    const SizedBox(height: 22),
    sectionTitle('負債異動', '${liabilityTx.length} 筆'),
    for (final t in liabilityTx)
      dismissible(
        key: 'debt-${t['id']}',
        onDelete: () =>
            mutate('/api/liability-transactions/${t['id']}', method: 'DELETE'),
        child: Card(
          child: ListTile(
            title: Text('${t['name']} · ${liabilityType(t['type'])}'),
            subtitle: Text('${t['tradedAt']}  ${t['note'] ?? ''}'),
            trailing: Text(money(t['amount'], '${t['currency']}')),
          ),
        ),
      ),
  ]);

  Widget _alerts() => _scroll([
    Row(
      children: [
        Expanded(
          child: sectionTitle(
            '價格提醒',
            '${alerts.where((x) => x['active'] == true).length} 個啟用',
          ),
        ),
        OutlinedButton.icon(
          onPressed: _checkAlerts,
          icon: const Icon(Icons.sync),
          label: const Text('立即檢查'),
        ),
      ],
    ),
    if (alerts.isEmpty) empty('尚未建立價格提醒'),
    for (final a in alerts)
      Card(
        child: SwitchListTile(
          value: a['active'] == true,
          onChanged: (v) => mutate(
            '/api/alerts/${a['id']}',
            method: 'PUT',
            body: {'active': v},
          ),
          title: Text('${a['name']} · ${alertType(a['ruleType'])}'),
          subtitle: Text(
            '觸發價 ${money(a['triggerPrice'], '${a['currency']}')} · 最新 ${money(a['lastPrice'], '${a['currency']}')}',
          ),
          secondary: IconButton(
            icon: const Icon(Icons.delete_outline),
            onPressed: () => confirmDelete(
              () => mutate('/api/alerts/${a['id']}', method: 'DELETE'),
            ),
          ),
        ),
      ),
    const SizedBox(height: 22),
    sectionTitle('觸發紀錄', '${events.length} 筆'),
    for (final e in events)
      Card(
        child: ListTile(
          leading: Icon(
            e['isRead'] == true
                ? Icons.notifications_none
                : Icons.notification_important,
            color: e['isRead'] == true
                ? null
                : Theme.of(context).colorScheme.error,
          ),
          title: Text('${e['name']} · ${alertType(e['ruleType'])}'),
          subtitle: Text(
            '${e['triggeredAt']} · 市價 ${money(e['marketPrice'], '${e['currency']}')}',
          ),
          onTap: e['isRead'] == true
              ? null
              : () =>
                    mutate('/api/alert-events/${e['id']}/read', method: 'POST'),
        ),
      ),
  ]);

  Widget sectionTitle(String text, String count) => Padding(
    padding: const EdgeInsets.only(bottom: 10),
    child: Row(
      children: [
        Expanded(
          child: Text(
            text,
            style: Theme.of(
              context,
            ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700),
          ),
        ),
        Text(count),
      ],
    ),
  );
  Widget empty(String text) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 42),
    child: Center(child: Text(text)),
  );
  Widget dismissible({
    required String key,
    required Future<void> Function() onDelete,
    required Widget child,
  }) => Dismissible(
    key: ValueKey(key),
    direction: DismissDirection.endToStart,
    confirmDismiss: (_) => ask('確定刪除這筆資料？'),
    onDismissed: (_) => onDelete(),
    background: Container(
      color: Theme.of(context).colorScheme.error,
      alignment: Alignment.centerRight,
      padding: const EdgeInsets.all(24),
      child: const Icon(Icons.delete, color: Colors.white),
    ),
    child: child,
  );

  Widget? _fab() => switch (page) {
    1 => FloatingActionButton.extended(
      onPressed: _trade,
      icon: const Icon(Icons.add),
      label: const Text('新增交易'),
    ),
    2 => FloatingActionButton.extended(
      onPressed: cash.isEmpty ? _cashAccount : () => _cashEntry(),
      icon: const Icon(Icons.add),
      label: const Text('新增流水'),
    ),
    3 => FloatingActionButton.extended(
      onPressed: liabilities.isEmpty ? _liability : () => _liabilityEntry(),
      icon: const Icon(Icons.add),
      label: const Text('新增異動'),
    ),
    4 => FloatingActionButton.extended(
      onPressed: _alert,
      icon: const Icon(Icons.add_alert),
      label: const Text('新增提醒'),
    ),
    _ => null,
  };

  Future<void> mutate(
    String path, {
    String method = 'POST',
    Map<String, dynamic>? body,
  }) async {
    try {
      await api.send(path, method: method, body: body);
      await reload();
    } catch (e) {
      if (mounted) snack(cleanError(e));
    }
  }

  Future<void> _refreshMarket() async {
    await mutate('/api/markets/refresh');
    if (mounted) snack('行情已更新');
  }

  Future<void> _checkAlerts() async {
    try {
      final r = map(await api.send('/api/alerts/check', method: 'POST'));
      await reload();
      if (mounted) snack('已檢查，觸發 ${r['triggered'] ?? 0} 個提醒');
    } catch (e) {
      if (mounted) snack(cleanError(e));
    }
  }

  Future<void> _settings() async {
    final c = TextEditingController(text: api.baseUrl);
    final value = await formDialog('伺服器設定', [
      field(c, 'API URL', hint: 'http://localhost:8080'),
    ]);
    if (value == true && c.text.trim().isNotEmpty) {
      api.baseUrl = c.text.trim();
      reload();
    }
  }

  Future<void> _trade({Map<String, dynamic>? existing}) async {
    final symbol = TextEditingController(text: '${existing?['symbol'] ?? ''}');
    final name = TextEditingController(text: '${existing?['name'] ?? ''}');
    final quantity = TextEditingController(
      text: '${existing?['quantity'] ?? ''}',
    );
    final price = TextEditingController(text: '${existing?['price'] ?? ''}');
    final fee = TextEditingController(text: '${existing?['fee'] ?? 0}');
    final note = TextEditingController(text: '${existing?['note'] ?? ''}');
    String type = '${existing?['type'] ?? 'buy'}',
        market = '${existing?['market'] ?? 'TW'}';
    final ok = await formDialog(existing == null ? '新增交易' : '編輯交易', [
      field(symbol, 'Ticker'),
      field(name, '名稱'),
      StatefulBuilder(
        builder: (_, set) => Row(
          children: [
            Expanded(
              child: dropdown('買賣', type, const {
                'buy': '買入',
                'sell': '賣出',
              }, (v) => set(() => type = v!)),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: dropdown('市場', market, const {
                'TW': '台股',
                'US': '美股',
              }, (v) => set(() => market = v!)),
            ),
          ],
        ),
      ),
      Row(
        children: [
          Expanded(child: field(quantity, '數量', number: true)),
          const SizedBox(width: 12),
          Expanded(child: field(price, '成交價', number: true)),
        ],
      ),
      field(fee, '手續費', number: true),
      field(note, '投資筆記', lines: 3),
    ]);
    if (ok != true) return;
    final body = {
      'symbol': symbol.text,
      'name': name.text,
      'type': type,
      'quantity': doubleVal(quantity),
      'price': doubleVal(price),
      'fee': doubleVal(fee),
      'tradedAt': today(),
      'note': note.text,
      'market': market,
      'currency': market == 'US' ? 'USD' : 'TWD',
      'assetType': 'stock',
    };
    await mutate(
      existing == null
          ? '/api/transactions'
          : '/api/transactions/${existing['id']}',
      method: existing == null ? 'POST' : 'PUT',
      body: body,
    );
  }

  Future<void> _price(Map<String, dynamic> h) async {
    final c = TextEditingController(text: '${h['currentPrice']}');
    if (await formDialog('更新 ${h['symbol']} 市價', [
          field(c, '目前價格', number: true),
        ]) ==
        true) {
      await mutate(
        '/api/assets/${Uri.encodeComponent('${h['symbol']}')}/price',
        method: 'PUT',
        body: {'price': doubleVal(c)},
      );
    }
  }

  Future<void> _cashAccount() async {
    final name = TextEditingController(),
        balance = TextEditingController(text: '0'),
        note = TextEditingController();
    String unit = 'TWD';
    final ok = await formDialog('新增現金帳戶', [
      field(name, '帳戶名稱'),
      StatefulBuilder(
        builder: (_, set) => dropdown('幣別', unit, const {
          'TWD': 'TWD',
          'USD': 'USD',
        }, (v) => set(() => unit = v!)),
      ),
      field(balance, '期初餘額', number: true),
      field(note, '備註'),
    ]);
    if (ok == true) {
      await mutate(
        '/api/cash-accounts',
        body: {
          'name': name.text,
          'currency': unit,
          'initialBalance': doubleVal(balance),
          'note': note.text,
        },
      );
    }
  }

  Future<void> _cashEntry({Map<String, dynamic>? account}) async {
    if (cash.isEmpty) return _cashAccount();
    int id = (account?['id'] ?? cash.first['id']) as int;
    String type = 'deposit';
    final amount = TextEditingController(), note = TextEditingController();
    final ok = await formDialog('新增現金流水', [
      StatefulBuilder(
        builder: (_, set) => dropdown('帳戶', '$id', {
          for (final x in cash) '${x['id']}': '${x['name']}',
        }, (v) => set(() => id = int.parse(v!))),
      ),
      StatefulBuilder(
        builder: (_, set) => dropdown('類型', type, const {
          'deposit': '存入',
          'withdrawal': '提出',
          'dividend': '股息',
          'interest': '利息',
          'fee': '費用',
          'adjustment': '調整',
        }, (v) => set(() => type = v!)),
      ),
      field(amount, '金額', number: true),
      field(note, '備註'),
    ]);
    if (ok == true) {
      await mutate(
        '/api/cash-transactions',
        body: {
          'accountId': id,
          'type': type,
          'amount': doubleVal(amount),
          'tradedAt': today(),
          'note': note.text,
        },
      );
    }
  }

  Future<void> _liability() async {
    final name = TextEditingController(),
        balance = TextEditingController(text: '0'),
        rate = TextEditingController(text: '0'),
        note = TextEditingController();
    String category = 'loan', unit = 'TWD';
    final ok = await formDialog('新增負債', [
      field(name, '名稱'),
      StatefulBuilder(
        builder: (_, set) => dropdown('類別', category, const {
          'mortgage': '房貸',
          'loan': '貸款',
          'credit_card': '信用卡',
          'other': '其他',
        }, (v) => set(() => category = v!)),
      ),
      StatefulBuilder(
        builder: (_, set) => dropdown('幣別', unit, const {
          'TWD': 'TWD',
          'USD': 'USD',
        }, (v) => set(() => unit = v!)),
      ),
      field(balance, '期初餘額', number: true),
      field(rate, '年利率 %', number: true),
      field(note, '備註'),
    ]);
    if (ok == true) {
      await mutate(
        '/api/liabilities',
        body: {
          'name': name.text,
          'category': category,
          'currency': unit,
          'initialBalance': doubleVal(balance),
          'interestRate': doubleVal(rate),
          'note': note.text,
        },
      );
    }
  }

  Future<void> _liabilityEntry({Map<String, dynamic>? item}) async {
    if (liabilities.isEmpty) return _liability();
    int id = (item?['id'] ?? liabilities.first['id']) as int;
    String type = 'repay';
    final amount = TextEditingController(), note = TextEditingController();
    final ok = await formDialog('新增負債異動', [
      StatefulBuilder(
        builder: (_, set) => dropdown('負債', '$id', {
          for (final x in liabilities) '${x['id']}': '${x['name']}',
        }, (v) => set(() => id = int.parse(v!))),
      ),
      StatefulBuilder(
        builder: (_, set) => dropdown('類型', type, const {
          'borrow': '新增借款',
          'repay': '還款',
          'interest': '利息',
          'adjustment': '調整',
        }, (v) => set(() => type = v!)),
      ),
      field(amount, '金額', number: true),
      field(note, '備註'),
    ]);
    if (ok == true) {
      await mutate(
        '/api/liability-transactions',
        body: {
          'liabilityId': id,
          'type': type,
          'amount': doubleVal(amount),
          'tradedAt': today(),
          'note': note.text,
        },
      );
    }
  }

  Future<void> _alert() async {
    final holdings = maps(portfolio['holdings']);
    if (holdings.isEmpty) {
      snack('請先新增持股');
      return;
    }
    String symbol = '${holdings.first['symbol']}', rule = 'above';
    final value = TextEditingController();
    final ok = await formDialog('新增價格提醒', [
      StatefulBuilder(
        builder: (_, set) => dropdown('標的', symbol, {
          for (final x in holdings)
            '${x['symbol']}': '${x['name']} (${x['symbol']})',
        }, (v) => set(() => symbol = v!)),
      ),
      StatefulBuilder(
        builder: (_, set) => dropdown('條件', rule, const {
          'above': '高於價格',
          'below': '低於價格',
          'take_profit_percent': '成本停利 %',
          'stop_loss_percent': '成本停損 %',
          'trailing_stop_percent': '移動停損 %',
        }, (v) => set(() => rule = v!)),
      ),
      field(value, '價格或比例', number: true),
    ]);
    if (ok == true) {
      await mutate(
        '/api/alerts',
        body: {
          'symbol': symbol,
          'ruleType': rule,
          'value': doubleVal(value),
          'oneShot': true,
        },
      );
    }
  }

  Widget field(
    TextEditingController c,
    String label, {
    bool number = false,
    int lines = 1,
    String? hint,
  }) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: TextField(
      controller: c,
      maxLines: lines,
      keyboardType: number
          ? const TextInputType.numberWithOptions(decimal: true)
          : TextInputType.text,
      decoration: InputDecoration(labelText: label, hintText: hint),
    ),
  );
  Widget dropdown(
    String label,
    String value,
    Map<String, String> values,
    ValueChanged<String?> onChanged,
  ) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: DropdownButtonFormField<String>(
      initialValue: value,
      decoration: InputDecoration(labelText: label),
      items: [
        for (final e in values.entries)
          DropdownMenuItem(value: e.key, child: Text(e.value)),
      ],
      onChanged: onChanged,
    ),
  );
  Future<bool?> formDialog(String title, List<Widget> fields) =>
      showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(title),
          content: SizedBox(
            width: 460,
            child: SingleChildScrollView(
              child: Column(mainAxisSize: MainAxisSize.min, children: fields),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('儲存'),
            ),
          ],
        ),
      );
  Future<bool> ask(String text) async =>
      (await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          content: Text(text),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('確定'),
            ),
          ],
        ),
      )) ??
      false;
  Future<void> confirmDelete(Future<void> Function() action) async {
    if (await ask('確定刪除？')) await action();
  }

  void snack(String text) =>
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));
  String signedMoney(dynamic v) {
    final n = numValue(v);
    return '${n > 0 ? '+' : ''}${money(n)}';
  }

  Color gainColor(double n) =>
      n >= 0 ? const Color(0xff168557) : const Color(0xffc04343);
}

List<Map<String, dynamic>> maps(dynamic value) => value is List
    ? value.map((x) => Map<String, dynamic>.from(x as Map)).toList()
    : [];
Map<String, dynamic> map(dynamic value) =>
    value is Map ? Map<String, dynamic>.from(value) : {};
double numValue(dynamic v) =>
    v is num ? v.toDouble() : double.tryParse('$v') ?? 0;
double doubleVal(TextEditingController c) => double.tryParse(c.text) ?? 0;
String today() {
  final d = DateTime.now();
  return '${d.year}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';
}

String cleanError(Object e) => e.toString().replaceFirst('Exception: ', '');
String cashType(dynamic x) =>
    const {
      'deposit': '存入',
      'withdrawal': '提出',
      'dividend': '股息',
      'interest': '利息',
      'fee': '費用',
      'adjustment': '調整',
    }['$x'] ??
    '$x';
String liabilityType(dynamic x) =>
    const {
      'borrow': '新增借款',
      'repay': '還款',
      'interest': '利息',
      'adjustment': '調整',
    }['$x'] ??
    '$x';
String alertType(dynamic x) =>
    const {
      'above': '高於價格',
      'below': '低於價格',
      'take_profit_percent': '成本停利',
      'stop_loss_percent': '成本停損',
      'trailing_stop_percent': '移動停損',
    }['$x'] ??
    '$x';
